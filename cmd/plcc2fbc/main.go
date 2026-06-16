/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

var errPackageNotFound = errors.New("no FBC data generated")

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errPackageNotFound) {
			os.Exit(2)
		}
		log.Fatal(err)
	}
}

func run() (err error) {
	var format string
	var logPath string
	var packages string
	var dumpPLCC bool
	var inputPath string
	var strict bool
	var validatorsFlag string
	var listValidators bool

	flag.StringVarP(&format, "output", "o", "json", "output format: json, json-pretty, or yaml")
	flag.StringVarP(&logPath, "log", "l", "", "write operational logs to a file; parent directory must exist (default: stdout)")
	flag.StringVarP(&packages, "package", "p", "", "comma-separated package names to process (default: all)")
	flag.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.BoolVar(&dumpPLCC, "dump-plcc", false, "dump filtered PLCC JSON instead of generating FBC")
	flag.BoolVar(&strict, "strict", false, "treat PLCC validation warnings as errors and filter out failing packages")
	flag.StringVar(&validatorsFlag, "validators", "all", "comma-separated list of validators to run (labels, groups: all, syntax, semantic, catalog)")
	flag.BoolVar(&listValidators, "list-validators", false, "list available validators and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <output-file>\n\nThe parent directory of <output-file> must already exist.\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if listValidators {
		fmt.Print(plcc.ListValidators())
		return nil
	}

	var logWriter io.Writer = os.Stdout
	if logPath != "" {
		if err := validateOutputPath(logPath); err != nil {
			return fmt.Errorf("invalid log path: %w", err)
		}
		lf, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer func() { _ = lf.Close() }()
		logWriter = lf
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter, nil)))

	if flag.NArg() != 1 {
		flag.Usage()
		return fmt.Errorf("missing output file")
	}
	writePath := flag.Arg(0)
	if err := validateOutputPath(writePath); err != nil {
		return fmt.Errorf("invalid output path: %w", err)
	}

	writer, err := fbc.NewPackageWriter(format)
	if err != nil {
		slog.Error("invalid output format", "flag", "-o", "value", format, "allowed", "json,json-pretty,yaml")
		return err
	}

	var catalog *plcc.Catalog
	if inputPath != "" {
		catalog, err = plcc.Load(inputPath)
	} else {
		catalog, err = plcc.Fetch()
	}
	if err != nil {
		slog.Error("failed to load PLCC data", "error", err)
		return err
	}

	slog.Info("fetched products from PLCC", "count", catalog.Len())
	if packages != "" {
		var names []string
		for _, name := range strings.Split(packages, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
		notFound := catalog.FilterByPackageNames(names)
		for _, name := range notFound {
			slog.Warn("requested package not found in PLCC data", "package", name)
		}
	} else {
		catalog.FilterPackages()
	}
	slog.Info("filtered packages", "count", catalog.Len())
	catalog.SortByPackage()

	// Resolve which validators to run.
	var validatorNames []string
	for _, name := range strings.Split(validatorsFlag, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			validatorNames = append(validatorNames, name)
		}
	}
	validators, catalogValidators, err := plcc.LookupValidators(validatorNames...)
	if err != nil {
		slog.Error("invalid --validators flag", "error", err)
		return err
	}

	// Run catalog-level PLCC validators (cross-product checks).
	if len(catalogValidators) > 0 {
		before := catalog.Len()
		for pkg, reasons := range catalog.Validate(strict, catalogValidators...) {
			if err := report.LogResults(os.Stderr, report.ValidationResult{
				PackageName: pkg,
				Valid:       !strict,
				Reasons:     reasons,
			}); err != nil {
				slog.Error("failed to write catalog validation warnings", "package", pkg, "error", err)
			}
		}
		if strict {
			slog.Info("strict PLCC catalog validation", "passed", catalog.Len(), "filtered", before-catalog.Len())
		}
	}

	// Run per-product PLCC validators. With --strict, failing packages are filtered out.
	var filtered []plcc.Product
	for _, product := range catalog.Data {
		warnings := plcc.ValidateProduct(product, validators...)
		if len(warnings) == 0 {
			filtered = append(filtered, product)
			continue
		}
		if err := report.LogResults(os.Stderr, report.ValidationResult{
			PackageName: product.Package,
			Valid:       !strict,
			Reasons:     warnings,
		}); err != nil {
			slog.Error("failed to write validation warnings", "package", product.Package, "error", err)
		}
		if !strict {
			filtered = append(filtered, product)
		}
	}
	if strict {
		slog.Info("strict PLCC package validation", "passed", len(filtered), "filtered", len(catalog.Data)-len(filtered))
		catalog.Data = filtered
	}

	if dumpPLCC {
		if err := catalog.Dump(writePath); err != nil {
			slog.Error("failed to write PLCC dump", "path", writePath, "error", err)
			return err
		}
		slog.Info("wrote PLCC dump", "count", catalog.Len(), "path", writePath)
		return nil
	}

	f, err := os.Create(writePath)
	if err != nil {
		slog.Error("failed to create output file", "path", writePath, "error", err)
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing output file %s: %w", writePath, cerr)
		}
	}()

	blobCount, err := fbc.GenerateFBC(catalog.Data, f, os.Stderr, writer)
	if err != nil {
		slog.Error("failed to generate FBC", "error", err)
		return err
	}
	if blobCount == 0 {
		slog.Warn("no FBC data generated")
		return errPackageNotFound
	}
	slog.Info("wrote FBC data", "count", blobCount, "path", writePath, "format", format)
	return nil
}

func validateOutputPath(path string) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("parent directory %q does not exist", dir)
	}
	if err != nil {
		return fmt.Errorf("cannot access parent directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("parent path %q is not a directory", dir)
	}
	return nil
}
