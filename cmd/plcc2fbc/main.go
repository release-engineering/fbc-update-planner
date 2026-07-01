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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

var errNoFBCOutput = errors.New("no FBC data generated")

func main() {
	if err := run(); err != nil {
		var pkgErr *plcc.PackagesNotFoundError
		switch {
		case errors.As(err, &pkgErr):
			os.Exit(3)
		case errors.Is(err, errNoFBCOutput):
			os.Exit(2)
		default:
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
}

func run() error {
	var format string
	var logPath string
	var packages string
	var dumpPLCC bool
	var inputPath string
	var permissive bool
	var validatorsFlag string
	var listValidators bool
	var split bool

	flag.StringVarP(&format, "output", "o", "json", "output format: json, json-pretty, or yaml")
	flag.StringVarP(&logPath, "log", "l", "", "write validation/filtering report to a file; parent directory must exist (default: stderr)")
	flag.StringVarP(&packages, "package", "p", "", "comma-separated package names to process (default: all)")
	flag.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.BoolVar(&dumpPLCC, "dump-plcc", false, "dump filtered PLCC JSON instead of generating FBC")
	flag.BoolVar(&permissive, "permissive", false, "keep packages that fail PLCC validation instead of filtering them out")
	flag.StringVar(&validatorsFlag, "validators", "all", "comma-separated list of validators to run (labels, groups: all, syntax, semantic, catalog)")
	flag.BoolVar(&listValidators, "list-validators", false, "list available validators and exit")
	flag.BoolVar(&split, "split", false, "write each package to <dir>/<package>/lifecycle.{json,yaml}; positional arg is a directory")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <output-path>\n\nThe parent directory of <output-path> must already exist.\nWith --split, <output-path> must be an existing directory; partial output is not cleaned up on failure.\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	strict := !permissive

	if listValidators {
		fmt.Print(plcc.ListValidators())
		return nil
	}

	if dumpPLCC && split {
		return fmt.Errorf("--dump-plcc and --split are mutually exclusive")
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var reportWriter io.Writer = os.Stderr
	if logPath != "" {
		if err := validateOutputPath(logPath, false); err != nil {
			return fmt.Errorf("invalid log path: %w", err)
		}
		lf, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer func() { _ = lf.Close() }()
		reportWriter = lf
	}

	if flag.NArg() != 1 {
		flag.Usage()
		return fmt.Errorf("missing output path")
	}
	writePath := flag.Arg(0)
	if err := validateOutputPath(writePath, split); err != nil {
		return fmt.Errorf("invalid output path: %w", err)
	}

	catalog, err := loadAndValidate(inputPath, packages, validatorsFlag, strict, reportWriter)
	if err != nil {
		var pkgErr *plcc.PackagesNotFoundError
		if errors.As(err, &pkgErr) {
			for _, name := range pkgErr.Names {
				slog.Error("requested package not found in PLCC data", "package", name)
			}
		}
		return err
	}

	if dumpPLCC {
		if err := catalog.Dump(writePath); err != nil {
			return fmt.Errorf("failed to write PLCC dump to %s: %w", writePath, err)
		}
		slog.Info("wrote PLCC dump", "count", catalog.Len(), "path", writePath)
		return nil
	}

	writer, err := fbc.NewPackageWriter(format)
	if err != nil {
		return fmt.Errorf("invalid output format %q (allowed: json, json-pretty, yaml): %w", format, err)
	}

	var count int
	if split {
		count, err = writeSplit(catalog.Data, writePath, writer, reportWriter)
	} else {
		count, err = writeFile(catalog.Data, writePath, writer, reportWriter)
	}
	if err != nil {
		if errors.Is(err, errNoFBCOutput) {
			slog.Error("no FBC data generated")
		}
		return err
	}
	slog.Info("wrote FBC data", "count", count, "path", writePath)
	return nil
}

func writeSplit(products []plcc.Product, dir string, writer fbc.PackageWriter, reportWriter io.Writer) (int, error) {
	if len(products) == 0 {
		return 0, errNoFBCOutput
	}

	filename := "lifecycle." + writer.Ext()

	for _, product := range products {
		if !fs.ValidPath(product.Package) {
			return 0, fmt.Errorf("unsafe package name %q: would escape output directory", product.Package)
		}
		pkgDir := filepath.Join(dir, product.Package)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return 0, fmt.Errorf("creating package directory %s: %w", pkgDir, err)
		}
		outPath := filepath.Join(pkgDir, filename)
		f, err := os.Create(outPath)
		if err != nil {
			return 0, fmt.Errorf("creating output file %s: %w", outPath, err)
		}
		count, werr := fbc.GenerateFBC([]plcc.Product{product}, f, reportWriter, writer)
		cerr := f.Close()
		if werr != nil {
			_ = os.Remove(outPath)
			return 0, fmt.Errorf("writing package %s: %w", product.Package, werr)
		}
		if cerr != nil {
			return 0, fmt.Errorf("closing %s: %w", outPath, cerr)
		}
		// Safeguard: a single product should always produce exactly one FBC blob.
		if count != 1 {
			_ = os.Remove(outPath)
			return 0, fmt.Errorf("package %s produced %d FBC blobs, expected 1", product.Package, count)
		}
	}

	return len(products), nil
}

func writeFile(products []plcc.Product, path string, writer fbc.PackageWriter, reportWriter io.Writer) (count int, err error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("creating output file %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing output file %s: %w", path, cerr)
		}
	}()

	blobCount, err := fbc.GenerateFBC(products, f, reportWriter, writer)
	if err != nil {
		return 0, err
	}
	if blobCount == 0 {
		return 0, errNoFBCOutput
	}
	return blobCount, nil
}

func loadAndValidate(inputPath, packages, validatorsFlag string, strict bool, reportWriter io.Writer) (*plcc.Catalog, error) {
	var catalog *plcc.Catalog
	var err error
	if inputPath != "" {
		catalog, err = plcc.Load(inputPath)
	} else {
		catalog, err = plcc.Fetch()
	}
	if err != nil {
		return nil, fmt.Errorf("loading PLCC data: %w", err)
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
		if err := catalog.FilterByPackageNames(names); err != nil {
			if strict {
				return nil, err
			}
			var pkgErr *plcc.PackagesNotFoundError
			if !errors.As(err, &pkgErr) {
				return nil, err
			}
			for _, name := range pkgErr.Names {
				slog.Warn("requested package not found in PLCC data", "package", name)
			}
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
		return nil, fmt.Errorf("invalid --validators flag: %w", err)
	}

	if len(catalogValidators) > 0 {
		before := catalog.Len()
		for pkg, reasons := range catalog.Validate(strict, catalogValidators...) {
			if err := report.LogResults(reportWriter, report.ValidationResult{
				PackageName: pkg,
				Valid:       !strict,
				Reasons:     reasons,
			}); err != nil {
				slog.Error("failed to write catalog validation warnings", "package", pkg, "error", err)
			}
		}
		if strict {
			slog.Info("PLCC catalog validation", "passed", catalog.Len(), "filtered", before-catalog.Len())
		}
	}

	var filtered []plcc.Product
	for _, product := range catalog.Data {
		warnings := plcc.ValidateProduct(product, validators...)
		if len(warnings) == 0 {
			filtered = append(filtered, product)
			continue
		}
		if err := report.LogResults(reportWriter, report.ValidationResult{
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
		slog.Info("PLCC package validation", "passed", len(filtered), "filtered", len(catalog.Data)-len(filtered))
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
