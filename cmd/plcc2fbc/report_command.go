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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/pkg/classify"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func runReportCommand(args []string) error {
	var inputPath, packages, validatorsFlag string
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flags.StringVarP(&packages, "package", "p", "", "comma-separated package names to assess (default: all)")
	flags.StringVar(&validatorsFlag, "validators", "all", "comma-separated list of validators to run (labels, groups: all, none, syntax, semantic, catalog)")
	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s report [flags] <catalog-data.json> <output.json>\n\nThe parent directory of <output.json> must already exist.\n\nFlags:\n", os.Args[0])
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 2 {
		flags.Usage()
		return fmt.Errorf("report requires catalog data and output paths")
	}
	if err := validateOutputPath(flags.Arg(1), false); err != nil {
		return fmt.Errorf("invalid output path: %w", err)
	}

	slog.Info("plcc2fbc starting", "version", versionString(), "validators", validatorsFlag)
	rawCatalog, err := loadCatalog(inputPath)
	if err != nil {
		return err
	}
	// Validator initialization needs the full catalog for cross-product
	// context, including the OCP product that has no package name.
	validators, catalogValidators, err := rawCatalog.LookupValidators(parseValidatorNames(validatorsFlag)...)
	if err != nil {
		return fmt.Errorf("invalid --validators flag: %w", err)
	}
	rawCatalog.DropWithoutPackageName()
	rawCatalog.SortByPackage()
	return runReport(rawCatalog, flags.Arg(0), packages, flags.Arg(1), validators, catalogValidators)
}

// catalogDataFile is the JSON structure written by the shell script and
// read by the report command. It captures lifecycle package names plus
// lifecycle and bundle version maps extracted from opm render output.
type catalogDataFile struct {
	SchemaVersion     string              `json:"schemaVersion,omitempty"`
	LifecyclePackages []string            `json:"lifecyclePackages"`
	LifecycleVersions map[string][]string `json:"lifecycleVersions"`
	BundleVersions    map[string][]string `json:"bundleVersions"`
}

func loadCatalogData(path string) (*classify.CatalogData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading catalog data: %w", err)
	}
	var cdf catalogDataFile
	if err := json.Unmarshal(data, &cdf); err != nil {
		return nil, fmt.Errorf("decoding catalog data: %w", err)
	}

	cd := &classify.CatalogData{
		LifecyclePackages: make(map[string]bool),
		LifecycleVersions: make(map[string]map[string]bool),
		BundleVersions:    make(map[string]map[string]bool),
	}
	for _, pkg := range cdf.LifecyclePackages {
		cd.LifecyclePackages[pkg] = true
	}
	for pkg, versions := range cdf.LifecycleVersions {
		cd.LifecycleVersions[pkg] = make(map[string]bool, len(versions))
		for _, v := range versions {
			cd.LifecycleVersions[pkg][v] = true
		}
	}
	for pkg, versions := range cdf.BundleVersions {
		cd.BundleVersions[pkg] = make(map[string]bool, len(versions))
		for _, v := range versions {
			cd.BundleVersions[pkg][v] = true
		}
	}
	return cd, nil
}

func runReport(catalog *plcc.Catalog, catalogDataPath, packages, writePath string, validators []plcc.Validator, catalogValidators []plcc.CatalogValidator) (err error) {
	cd, loadErr := loadCatalogData(catalogDataPath)
	if loadErr != nil {
		return loadErr
	}

	var pkgList []string
	if packages != "" {
		seen := make(map[string]bool)
		for _, name := range strings.Split(packages, ",") {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				seen[name] = true
				pkgList = append(pkgList, name)
			}
		}
		// Warn about -p packages not found in PLCC data. The report command
		// intentionally does not error on missing packages (unlike the
		// normal pipeline's exit-code-3 behavior) because classification
		// of missing-from-PLCC packages is a core use case. The warning
		// keeps the user informed.
		plccPkgs := make(map[string]bool)
		for _, p := range catalog.Data {
			for _, pkg := range p.Packages() {
				plccPkgs[pkg] = true
			}
		}
		for _, name := range pkgList {
			if !plccPkgs[name] {
				slog.Warn("requested package not found in PLCC data (will be classified as PLCC missing)", "package", name)
			}
		}
	}

	reports := classify.Classify(classify.Input{
		Catalog:           catalog,
		CatalogData:       cd,
		Packages:          pkgList,
		Validators:        validators,
		CatalogValidators: catalogValidators,
	})

	f, err := os.Create(writePath)
	if err != nil {
		return fmt.Errorf("creating report file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reports); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	slog.Info("wrote classification report", "count", len(reports), "path", writePath)
	return nil
}
