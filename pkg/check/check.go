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

// Package check runs the daily operator lifecycle assessment and renders its
// artifacts from one in-memory result model.
package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/assessment"
	"github.com/release-engineering/fbc-update-planner/pkg/classify"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

type Options struct {
	OutputDir    string
	InputPath    string
	Operators    string
	Validators   string
	CatalogImage string
	ValidateOnly bool
	Webhook      string
}

type runData struct {
	options Options
	names   []string
	reports []classify.OperatorReport
	hasFBC  bool
	runURL  string
}

// Run writes the requested artifacts and prints exactly the summary.txt
// content to stdout. Fatal input and opm errors leave a nonzero exit to the
// caller; a run with no translatable FBC packages still produces a report.
func Run(ctx context.Context, opts Options, stdout, stderr io.Writer) error {
	if opts.OutputDir == "" {
		opts.OutputDir = "."
	}
	if opts.Validators == "" {
		opts.Validators = "all"
	}
	requested, err := readOperators(opts.Operators)
	if err != nil {
		return err
	}
	if err := validateWebhook(opts.Webhook); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	logFile, err := os.Create(filepath.Join(opts.OutputDir, "slog.json"))
	if err != nil {
		return fmt.Errorf("creating operational log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	logger.Info("plcc-check starting", "validators", opts.Validators)

	var raw *plcc.Catalog
	if opts.InputPath == "" {
		raw, err = plcc.Fetch()
	} else {
		raw, err = plcc.Load(opts.InputPath)
	}
	if err != nil {
		return fmt.Errorf("loading PLCC data: %w", err)
	}
	logger.Info("loaded PLCC products", "count", raw.Len())
	validators, catalogValidators, err := raw.LookupValidators(commaNames(opts.Validators)...)
	if err != nil {
		return fmt.Errorf("invalid --validators flag: %w", err)
	}
	logger.Info("resolved validators", "product", len(validators), "catalog", len(catalogValidators))
	validated, err := assessment.Validate(raw, assessment.ValidationOptions{
		Packages:          requested,
		SelectPackages:    opts.Operators != "",
		Validators:        validators,
		CatalogValidators: catalogValidators,
		Strict:            true,
		AllowMissing:      true,
	})
	if err != nil {
		return fmt.Errorf("validating PLCC data: %w", err)
	}
	for _, name := range validated.MissingPackages {
		logger.Warn("requested package not found in PLCC data", "package", name)
	}
	logger.Info("selected products", "count", validated.SelectedCount)
	logger.Info("PLCC catalog validation", "passed", validated.SelectedCount-validated.CatalogFiltered, "filtered", validated.CatalogFiltered)
	logger.Info("PLCC product validation", "passed", validated.Catalog.Len(), "filtered", validated.ProductFiltered)
	translate := !opts.ValidateOnly || opts.CatalogImage != ""
	evaluated := assessment.Evaluate(raw, validated, translate)
	if translate {
		logger.Info("FBC translation", "passed", len(evaluated.FBC), "filtered", len(evaluated.Failures))
	} else {
		logger.Info("skipped FBC translation in PLCC-only mode")
	}
	validation := append(append([]report.ValidationResult(nil), validated.Validation...), evaluated.Failures...)
	if err := writeValidation(filepath.Join(opts.OutputDir, "validation.jsonl"), validation); err != nil {
		return err
	}

	hasFBC := len(evaluated.FBC) > 0
	if opts.ValidateOnly {
		if err := validated.Catalog.Dump(filepath.Join(opts.OutputDir, "plcc-dump.json")); err != nil {
			return fmt.Errorf("writing PLCC dump: %w", err)
		}
	} else if hasFBC {
		if err := writeFBC(filepath.Join(opts.OutputDir, "fbc-output.yaml"), evaluated.FBC); err != nil {
			return err
		}
	} else {
		logger.Warn("no FBC data generated")
		if _, err := fmt.Fprintln(stderr, "FBC output not found: no valid packages"); err != nil {
			return fmt.Errorf("printing FBC warning: %w", err)
		}
	}

	var catalogData *classify.CatalogData
	if opts.CatalogImage != "" {
		catalogData, err = renderCatalog(ctx, opts.CatalogImage)
		if err != nil {
			return err
		}
		logger.Info("catalog rendered", "lifecyclePackages", len(catalogData.LifecyclePackages), "bundlePackages", len(catalogData.BundleVersions))
		if err := writeCatalogPackages(filepath.Join(opts.OutputDir, "catalog-packages.txt"), catalogData); err != nil {
			return err
		}
	} else {
		catalogData = &classify.CatalogData{}
	}

	names := requested
	if opts.Operators == "" {
		names = allNames(evaluated, catalogData)
	}
	reports := classify.Classify(classify.Input{
		Catalog:     raw,
		Evaluated:   &evaluated,
		CatalogData: catalogData,
		Packages:    names,
	})
	if opts.CatalogImage != "" {
		if err := writeJSON(filepath.Join(opts.OutputDir, "classification.json"), reports); err != nil {
			return err
		}
	}
	data := runData{options: opts, names: names, reports: reports, hasFBC: hasFBC}
	summary := renderSummary(data)
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "summary.txt"), []byte(summary), 0o644); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}
	if _, err := io.WriteString(stdout, summary); err != nil {
		return fmt.Errorf("printing summary: %w", err)
	}
	if opts.Webhook != "" {
		data.runURL = strings.TrimRight(os.Getenv("GITHUB_SERVER_URL"), "/") + "/" + os.Getenv("GITHUB_REPOSITORY") + "/actions/runs/" + os.Getenv("GITHUB_RUN_ID")
		if err := writeJSON(filepath.Join(opts.OutputDir, "slack-payload.json"), renderSlack(data)); err != nil {
			return err
		}
	}
	logger.Info("assessment complete", "operators", len(reports), "output", opts.OutputDir)
	return nil
}

func readOperators(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading operators file: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		if pos := strings.IndexByte(line, '#'); pos >= 0 {
			line = line[:pos]
		}
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no operator names found in %s", path)
	}
	return names, nil
}

func commaNames(value string) []string {
	var names []string
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func allNames(evaluated assessment.Result, cd *classify.CatalogData) []string {
	seen := make(map[string]bool)
	for name := range evaluated.Packages {
		seen[name] = true
	}
	for name := range cd.LifecyclePackages {
		seen[name] = true
	}
	for name := range cd.LifecycleVersions {
		seen[name] = true
	}
	for name := range cd.BundleVersions {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeValidation(path string, results []report.ValidationResult) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating validation report: %w", err)
	}
	if err := report.LogResults(f, results...); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeFBC(path string, packages []*fbc.Package) error {
	w, err := fbc.NewPackageWriter("yaml")
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating FBC output: %w", err)
	}
	if err := w.Write(f, packages...); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing FBC output: %w", err)
	}
	return f.Close()
}

func writeCatalogPackages(path string, cd *classify.CatalogData) error {
	names := make([]string, 0, len(cd.LifecyclePackages))
	for name := range cd.LifecyclePackages {
		names = append(names, name)
	}
	sort.Strings(names)
	content := ""
	if len(names) > 0 {
		content = strings.Join(names, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeJSON(path string, value any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}
