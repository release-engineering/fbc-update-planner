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
	"io/fs"
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

func run() error {
	var format string
	var logPath string
	var packages string
	var dumpPLCC bool
	var inputPath string
	var strict bool
	var validatorsFlag string
	var listValidators bool
	var split bool

	flag.StringVarP(&format, "output", "o", "json", "output format: json, json-pretty, or yaml")
	flag.StringVarP(&logPath, "log", "l", "", "write operational logs to a file; parent directory must exist (default: stdout)")
	flag.StringVarP(&packages, "package", "p", "", "comma-separated package names to process (default: all)")
	flag.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.BoolVar(&dumpPLCC, "dump-plcc", false, "dump filtered PLCC JSON instead of generating FBC")
	flag.BoolVar(&strict, "strict", false, "treat PLCC validation warnings as errors and filter out failing packages")
	flag.StringVar(&validatorsFlag, "validators", "all", "comma-separated list of validators to run (labels, groups: all, syntax, semantic, catalog)")
	flag.BoolVar(&listValidators, "list-validators", false, "list available validators and exit")
	flag.BoolVar(&split, "split", false, "write each package to <dir>/<package>/lifecycle.{json,yaml}; positional arg is a directory")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <output-path>\n\nThe parent directory of <output-path> must already exist.\nWith --split, <output-path> must be an existing directory; partial output is not cleaned up on failure.\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if listValidators {
		fmt.Print(plcc.ListValidators())
		return nil
	}

	if dumpPLCC && split {
		return fmt.Errorf("--dump-plcc and --split are mutually exclusive")
	}

	var logWriter io.Writer = os.Stdout
	if logPath != "" {
		if err := validateOutputPath(logPath, false); err != nil {
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
		return fmt.Errorf("missing output path")
	}
	writePath := flag.Arg(0)
	if err := validateOutputPath(writePath, split); err != nil {
		return fmt.Errorf("invalid output path: %w", err)
	}

	catalog, err := loadAndValidate(inputPath, packages, validatorsFlag, strict)
	if err != nil {
		return err
	}

	if dumpPLCC {
		if err := catalog.Dump(writePath); err != nil {
			slog.Error("failed to write PLCC dump", "path", writePath, "error", err)
			return err
		}
		slog.Info("wrote PLCC dump", "count", catalog.Len(), "path", writePath)
		return nil
	}

	writer, err := fbc.NewPackageWriter(format)
	if err != nil {
		slog.Error("invalid output format", "flag", "-o", "value", format, "allowed", "json,json-pretty,yaml")
		return err
	}

	if split {
		return writeSplit(catalog.Data, writePath, writer)
	}
	return writeFile(catalog.Data, writePath, writer)
}

func writeSplit(products []plcc.Product, dir string, writer fbc.PackageWriter) error {
	if len(products) == 0 {
		slog.Warn("no FBC data generated")
		return errPackageNotFound
	}

	filename := "lifecycle." + writer.Ext()

	var totalCount int
	for _, product := range products {
		if !fs.ValidPath(product.Package) {
			return fmt.Errorf("unsafe package name %q: would escape output directory", product.Package)
		}
		pkgDir := filepath.Join(dir, product.Package)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return fmt.Errorf("creating package directory %s: %w", pkgDir, err)
		}
		outPath := filepath.Join(pkgDir, filename)
		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("creating output file %s: %w", outPath, err)
		}
		count, werr := fbc.GenerateFBC([]plcc.Product{product}, f, os.Stderr, writer)
		cerr := f.Close()
		if werr != nil {
			_ = os.Remove(outPath)
			return fmt.Errorf("writing package %s: %w", product.Package, werr)
		}
		if cerr != nil {
			return fmt.Errorf("closing %s: %w", outPath, cerr)
		}
		if count == 0 {
			_ = os.Remove(outPath)
			return fmt.Errorf("package %s produced no FBC data", product.Package)
		}
		totalCount += count
	}

	slog.Info("wrote split FBC data", "count", totalCount, "dir", dir)
	return nil
}

func writeFile(products []plcc.Product, path string, writer fbc.PackageWriter) (err error) {
	f, err := os.Create(path)
	if err != nil {
		slog.Error("failed to create output file", "path", path, "error", err)
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing output file %s: %w", path, cerr)
		}
	}()

	blobCount, err := fbc.GenerateFBC(products, f, os.Stderr, writer)
	if err != nil {
		slog.Error("failed to generate FBC", "error", err)
		return err
	}
	if blobCount == 0 {
		slog.Warn("no FBC data generated")
		return errPackageNotFound
	}
	slog.Info("wrote FBC data", "count", blobCount, "path", path)
	return nil
}

func loadAndValidate(inputPath, packages, validatorsFlag string, strict bool) (*plcc.Catalog, error) {
	var catalog *plcc.Catalog
	var err error
	if inputPath != "" {
		catalog, err = plcc.Load(inputPath)
	} else {
		catalog, err = plcc.Fetch()
	}
	if err != nil {
		slog.Error("failed to load PLCC data", "error", err)
		return nil, err
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
		return nil, err
	}

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

	return catalog, nil
}

func validateOutputPath(path string, isDir bool) error {
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("directory %q does not exist", dir)
	}
	if err != nil {
		return fmt.Errorf("cannot access %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", dir)
	}
	return nil
}
