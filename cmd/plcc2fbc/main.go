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
			fmt.Fprintln(os.Stderr, "Error: requested packages not found in PLCC data")
			os.Exit(3)
		case errors.Is(err, errNoFBCOutput):
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(2)
		default:
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
}

func run() (err error) {
	var format string
	var logPath string
	var packages string
	var dumpPLCC bool
	var inputPath string
	var permissive bool
	var allowMissing bool
	var validatorsFlag string
	var listValidators bool
	var split bool

	flag.StringVarP(&format, "output", "o", "json", "output format: json, json-pretty, or yaml")
	flag.StringVarP(&logPath, "log", "l", "", "write validation/filtering report to a file; parent directory must exist (default: stderr)")
	flag.StringVarP(&packages, "package", "p", "", "comma-separated package names to process (default: all)")
	flag.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.BoolVar(&dumpPLCC, "dump-plcc", false, "dump filtered PLCC JSON instead of generating FBC")
	flag.BoolVar(&permissive, "permissive", false, "keep packages that fail PLCC validation instead of filtering them out")
	flag.BoolVar(&allowMissing, "allow-missing", false, "warn about missing -p packages instead of aborting")
	flag.StringVar(&validatorsFlag, "validators", "all", "comma-separated list of validators to run (labels, groups: all, none, syntax, semantic, catalog)")
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
		var lf *os.File
		lf, err = os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer func() {
			if cerr := lf.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("closing log file %s: %w", logPath, cerr)
			}
		}()
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

	var writer fbc.PackageWriter
	if !dumpPLCC {
		writer, err = fbc.NewPackageWriter(format)
		if err != nil {
			return fmt.Errorf("invalid output format: %w", err)
		}
	}

	catalog, err := loadAndValidate(inputPath, packages, validatorsFlag, strict, allowMissing, reportWriter)
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

	catalog.ExpandPackages()
	catalog.SortByPackage()

	var count int
	if split {
		count, err = writeSplitFBC(catalog.Data, writePath, writer, reportWriter)
	} else {
		count, err = writeFBC(catalog.Data, writePath, writer, reportWriter)
	}
	if err != nil {
		return err
	}
	slog.Info("wrote FBC data", "count", count, "path", writePath)
	return nil
}

func writeFBC(products []plcc.Product, path string, writer fbc.PackageWriter, reportWriter io.Writer) (count int, err error) {
	valid, failures := fbc.Translate(products, fbc.DefaultFilters()...)

	if err := report.LogResults(reportWriter, failures...); err != nil {
		return 0, err
	}

	if len(valid) == 0 {
		return 0, errNoFBCOutput
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("creating output file %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing output file %s: %w", path, cerr)
		}
	}()

	if err := writer.Write(f, valid...); err != nil {
		return 0, fmt.Errorf("writing packages: %w", err)
	}
	return len(valid), nil
}

func writeSplitFBC(products []plcc.Product, dir string, writer fbc.PackageWriter, reportWriter io.Writer) (int, error) {
	if len(products) == 0 {
		return 0, errNoFBCOutput
	}

	filters := fbc.DefaultFilters()

	for i, product := range products {
		if !fs.ValidPath(product.Package) {
			return i, fmt.Errorf("unsafe package name %q: would escape output directory", product.Package)
		}
		pkg, failure := fbc.TranslateProduct(product, filters...)
		if failure != nil {
			if err := report.LogResults(reportWriter, *failure); err != nil {
				return i, err
			}
			return i, fmt.Errorf("package %s failed FBC translation", product.Package)
		}
		if err := writePackageToDir(dir, product.Package, writer, pkg); err != nil {
			return i, err
		}
	}

	return len(products), nil
}

func writePackageToDir(dir, pkgName string, writer fbc.PackageWriter, pkg *fbc.Package) error {
	pkgDir := filepath.Join(dir, pkgName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return fmt.Errorf("creating package directory %s: %w", pkgDir, err)
	}
	outPath := filepath.Join(pkgDir, "lifecycle."+writer.Ext())
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file %s: %w", outPath, err)
	}
	werr := writer.Write(f, pkg)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("writing package %s: %w", pkgName, werr)
	}
	if cerr != nil {
		return fmt.Errorf("closing %s: %w", outPath, cerr)
	}
	return nil
}

func loadAndValidate(inputPath, packages, validatorsFlag string, strict, allowMissing bool, reportWriter io.Writer) (*plcc.Catalog, error) {
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

	var validatorNames []string
	for _, name := range strings.Split(validatorsFlag, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			validatorNames = append(validatorNames, name)
		}
	}
	validators, catalogValidators, err := catalog.LookupValidators(validatorNames...)
	if err != nil {
		return nil, fmt.Errorf("invalid --validators flag: %w", err)
	}
	slog.Info("resolved validators", "product", len(validators), "catalog", len(catalogValidators))

	if packages != "" {
		var names []string
		for _, name := range strings.Split(packages, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
		if err := catalog.FilterByPackageNames(names); err != nil {
			if !allowMissing {
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
		catalog.DropWithoutPackageName()
	}
	slog.Info("filtered packages", "count", catalog.Len())
	catalog.SortByPackage()

	if len(catalogValidators) > 0 {
		before := catalog.Len()
		for pkg, reasons := range catalog.Validate(strict, catalogValidators...) {
			if err := report.LogResults(reportWriter, report.ValidationResult{
				PackageName: pkg,
				Valid:       !strict,
				Reasons:     reasons,
			}); err != nil {
				return nil, fmt.Errorf("writing validation report for %s: %w", pkg, err)
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
			return nil, fmt.Errorf("writing validation report for %s: %w", product.Package, err)
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
