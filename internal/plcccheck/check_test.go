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

package plcccheck

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestApplySkipsPreservesAssessment(t *testing.T) {
	source, inventory := fixtures(t)
	names := []string{"full", "mixed", "validator", "bundle-only", "stale"}
	a := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: names, Validators: []string{"syntax"}})
	before, _ := json.Marshal(a)
	fbcBefore, _ := json.Marshal(a.FBC)
	plccBefore, _ := json.Marshal(a.FilteredPLCC)
	reasons := map[string]string{"not-selected": "Does not expand selection"}
	for _, name := range names {
		reasons[name] = "A shared exception"
	}
	if err := a.ApplySkips(reasons); err != nil {
		t.Fatal(err)
	}
	if len(a.Packages) != len(names) {
		t.Fatal("skip policy expanded selection")
	}
	for _, pkg := range a.Packages {
		if pkg.Action != Skipped || pkg.SkipReason != reasons[pkg.Name] {
			t.Fatalf("missing skip annotation: %+v", pkg)
		}
	}
	skipped, _ := json.Marshal(a)
	if err := a.ApplySkips(map[string]string{"full": " "}); err == nil {
		t.Fatal("accepted blank skip note")
	}
	unchanged, _ := json.Marshal(a)
	if !bytes.Equal(skipped, unchanged) {
		t.Fatal("invalid policy partially changed assessment")
	}
	if err := a.ApplySkips(nil); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(a)
	fbcAfter, _ := json.Marshal(a.FBC)
	plccAfter, _ := json.Marshal(a.FilteredPLCC)
	if !bytes.Equal(before, after) || !bytes.Equal(fbcBefore, fbcAfter) || !bytes.Equal(plccBefore, plccAfter) {
		t.Fatal("skip policy changed evidence or pipeline artifacts")
	}
}

func fixtures(t *testing.T) (*plcc.Catalog, *catalog.Inventory) {
	t.Helper()
	source, err := plcc.Load("testdata/plcc.json")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := catalog.Parse(bytes.NewReader(rendered))
	if err != nil {
		t.Fatal(err)
	}
	return source, inventory
}

func mustAssess(t *testing.T, source *plcc.Catalog, inventory *catalog.Inventory, options plcc.DatasetOptions) *Assessment {
	t.Helper()
	result, err := Assess(source, inventory, options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func findPackage(t *testing.T, result *Assessment, name string) PackageAssessment {
	t.Helper()
	for _, pkg := range result.Packages {
		if pkg.Name == name {
			return pkg
		}
	}
	t.Fatalf("package %q not found", name)
	return PackageAssessment{}
}

func versionIssue(kind IssueKind, version string) Issue {
	v, err := fbc.ParseMajorMinor(version)
	if err != nil {
		panic(err)
	}
	return Issue{Kind: kind, Version: &v}
}

func TestAssessFixtures(t *testing.T) {
	source, inventory := fixtures(t)
	result := mustAssess(t, source, inventory, plcc.DatasetOptions{Validators: []string{"syntax", "catalog"}})
	tests := []struct {
		name     string
		plcc     PLCCStatus
		coverage string
		action   Action
		issues   []Issue
	}{
		{"full", PLCCOK, "OK", OK, nil},
		{"mixed", PLCCIncomplete, "1/3", FixPLCC, []Issue{
			versionIssue(MissingCatalogLifecycleVersion, "1.3"), versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"),
		}},
		{"unpublished", PLCCOK, "MISSING", BuildOperator, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"),
		}},
		{"validator", PLCCInvalid, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"),
		}},
		{"converter", PLCCInvalid, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"),
			versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"),
		}},
		{"filter", PLCCInvalid, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"), versionIssue(MissingCatalogLifecycleVersion, "1.3"),
		}},
		{"alpha", PLCCInvalid, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"),
		}},
		{"beta", PLCCDuplicate, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingCatalogLifecycleVersion, "1.2"), versionIssue(MissingCatalogLifecycleVersion, "1.3"),
			versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"),
		}},
		{"no-bundles", PLCCOK, "NO BUNDLES", AddOperator, []Issue{{Kind: MissingCatalogBundles}, {Kind: MissingCatalogLifecycle}}},
		{"lifecycle-only", PLCCOK, "NO BUNDLES", AddOperator, []Issue{{Kind: MissingCatalogBundles}}},
		{"empty-lifecycle", PLCCOK, "0/1", BuildOperator, []Issue{versionIssue(MissingCatalogLifecycleVersion, "1.2")}},
		{"bundle-only", PLCCAbsent, "MISSING", AddPLCC, []Issue{
			{Kind: MissingPLCCPackage}, {Kind: MissingCatalogLifecycle}, versionIssue(MissingPLCCVersion, "1.2"), versionIssue(MissingCatalogLifecycleVersion, "1.2"),
		}},
		{"stale", PLCCAbsent, "NO BUNDLES", AddPLCC, []Issue{
			{Kind: MissingPLCCPackage}, {Kind: MissingCatalogBundles}, versionIssue(RegressedPLCCVersion, "1.2"),
		}},
		{"empty-plcc", PLCCInvalid, "MISSING", FixPLCC, []Issue{
			{Kind: MissingCatalogLifecycle}, versionIssue(MissingPLCCVersion, "1.2"), versionIssue(MissingCatalogLifecycleVersion, "1.2"),
		}},
	}
	if len(result.Packages) != len(tests) {
		t.Fatalf("got %d packages, want %d", len(result.Packages), len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := findPackage(t, result, tt.name)
			if pkg.PLCC != tt.plcc || pkg.Catalog.Status() != tt.coverage || pkg.Action != tt.action {
				t.Errorf("PLCC/coverage/action = %s/%s/%s, want %s/%s/%s", pkg.PLCC, pkg.Catalog.Status(), pkg.Action, tt.plcc, tt.coverage, tt.action)
			}
			if !reflect.DeepEqual(pkg.Issues, tt.issues) {
				t.Errorf("issues = %+v, want %+v", pkg.Issues, tt.issues)
			}
		})
	}
	full := findPackage(t, result, "full")
	if full.Catalog.Covered != 2 || len(full.Catalog.Bundles) != 4 || !slices.Equal(full.Catalog.BundleVersions, []fbc.MajorMinor{{Major: 1, Minor: 2}, {Major: 1, Minor: 10}}) {
		t.Errorf("patch collapse or numeric order lost: %+v", full.Catalog)
	}
	if !slices.Equal(full.Catalog.LifecycleVersions, full.Catalog.BundleVersions) {
		t.Error("lifecycle versions were not deduplicated and sorted")
	}
}

func TestAssessFailureEvidence(t *testing.T) {
	source, inventory := fixtures(t)
	tests := []struct {
		name       string
		validators []string
		stage      FailureStage
		label      string
		source     int
	}{
		{"validator", []string{"syntax", "catalog"}, PLCCValidation, "CUSTOM-01", 4},
		{"converter", []string{"syntax", "catalog"}, PLCCValidation, "REQ-DATE-03", 5},
		{"converter", []string{"none"}, FBCTranslation, "FBC-PHASE-01", 5},
		{"filter", []string{"none"}, FBCTranslation, "FBC-VAL-02", 6},
		{"empty-plcc", []string{"none"}, FBCTranslation, "FBC-VAL-01", 12},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+strings.Join(tt.validators, ","), func(t *testing.T) {
			result := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{tt.name}, Validators: tt.validators})
			pkg := result.Packages[0]
			if pkg.Action != FixPLCC || len(pkg.Failures) != 1 || len(pkg.ProducedVersions) != 0 {
				t.Fatalf("rejected package assessment = %+v", pkg)
			}
			failure := pkg.Failures[0]
			if failure.SourceIndex != tt.source || failure.Stage != tt.stage || !strings.Contains(strings.Join(failure.Reasons, "\n"), tt.label) {
				t.Errorf("failure lost evidence: %+v", failure)
			}
			if tt.stage == PLCCValidation {
				if failure.Validator == nil || failure.Validator.Label != tt.label || failure.Validator.Group != "syntax" || failure.Validator.Scope != plcc.ProductScope {
					t.Errorf("missing structured validator metadata: %+v", failure.Validator)
				}
			} else if failure.Validator != nil {
				t.Error("FBC failure was attributed to a PLCC validator")
			}
		})
	}
	// Disabling an optional rule changes the action; mandatory checks still ran above.
	result := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"validator"}, Validators: []string{"none"}})
	if len(result.Validators) != 0 || result.Packages[0].Action != BuildOperator {
		t.Errorf("disabled validation was not respected: %+v", result)
	}
}

func TestAssessAliasesAndDuplicateSourceProducts(t *testing.T) {
	source, inventory := fixtures(t)
	result := mustAssess(t, source, inventory, plcc.DatasetOptions{Validators: []string{"catalog"}})
	alpha := findPackage(t, result, "alpha")
	beta := findPackage(t, result, "beta")
	if !slices.Equal(beta.SourceProducts, []int{7, 8}) || !slices.Equal(beta.SourceVersions, []string{"1.2", "1.3"}) || len(beta.Failures) != 2 {
		t.Fatalf("duplicate products were collapsed: %+v", beta)
	}
	if len(alpha.Failures) != 1 || !slices.Equal(alpha.Failures[0].Packages, []string{"beta"}) || alpha.Failures[0].SourceIndex != 7 || alpha.PLCC != PLCCInvalid {
		t.Errorf("alias rejection lost the original target: %+v", alpha)
	}
	// Selection narrows aliases before catalog validation, matching Dataset policy.
	selected := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"alpha"}, Validators: []string{"catalog"}})
	if selected.Packages[0].Action != BuildOperator {
		t.Errorf("unselected beta duplicate rejected alpha: %+v", selected.Packages[0])
	}
	// With duplicate validation disabled, both whole products can contribute output.
	unvalidated := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"beta"}, Validators: []string{"none"}})
	got := unvalidated.Packages[0]
	if !slices.Equal(got.ProducedVersions, []fbc.MajorMinor{{Major: 1, Minor: 2}, {Major: 1, Minor: 3}}) || got.PLCC != PLCCIncomplete {
		t.Errorf("lost a duplicate product's version: %+v", got)
	}
	// A failure in another source product still blocks the package's primary remedy.
	source.Data[8].Versions[0].Phases = nil
	blocked := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"beta"}, Validators: []string{"none"}}).Packages[0]
	if blocked.Action != FixPLCC || blocked.PLCC != PLCCInvalid || len(blocked.Failures) != 1 || !reflect.DeepEqual(blocked.Issues, got.Issues) {
		t.Errorf("partial duplicate success concealed package failure: %+v", blocked)
	}
}

func TestAssessSelectionAndNoCatalog(t *testing.T) {
	source, inventory := fixtures(t)
	tests := []struct {
		name     string
		selected []string
		want     []string
	}{
		{"all", nil, []string{"alpha", "beta", "bundle-only", "converter", "empty-lifecycle", "empty-plcc", "filter", "full", "lifecycle-only", "mixed", "no-bundles", "stale", "unpublished", "validator"}},
		{"empty", []string{}, nil},
		{"explicit", []string{"mixed", "absent", "alpha", "mixed"}, []string{"mixed", "absent", "alpha"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: tt.selected, Validators: []string{"none"}})
			var names []string
			for _, pkg := range result.Packages {
				names = append(names, pkg.Name)
			}
			if !slices.Equal(names, tt.want) {
				t.Errorf("package order = %v, want %v", names, tt.want)
			}
		})
	}
	options := plcc.DatasetOptions{Packages: []string{"full", "converter", "absent"}, Validators: []string{"none"}}
	result := mustAssess(t, source, nil, options)
	for i, want := range []Action{OK, FixPLCC, AddPLCC} {
		pkg := result.Packages[i]
		var wantIssues []Issue
		if pkg.Name == "absent" {
			wantIssues = []Issue{{Kind: MissingPLCCPackage}}
		}
		if pkg.Action != want || pkg.Catalog != nil || !reflect.DeepEqual(pkg.Issues, wantIssues) {
			t.Errorf("without catalog: %+v, want %s with no catalog claims", pkg, want)
		}
	}
	empty := mustAssess(t, source, &catalog.Inventory{}, options)
	for i, want := range []Action{AddOperator, FixPLCC, AddPLCC} {
		if empty.Packages[i].Action != want || empty.Packages[i].Catalog.Status() != "NO BUNDLES" {
			t.Errorf("empty catalog: %+v, want %s", empty.Packages[i], want)
		}
	}
	all := mustAssess(t, source, nil, plcc.DatasetOptions{Validators: []string{"none"}})
	if len(all.Packages) != 12 {
		t.Errorf("no-catalog all selection has %d packages, want 12", len(all.Packages))
	}
}

func TestAssessPLCCHealthIndependentOfCoverage(t *testing.T) {
	source, inventory := fixtures(t)
	inventory.Packages["validator"] = catalog.Package{
		Bundles: []catalog.Bundle{{Name: "validator.v1.2.0", Version: "1.2.0"}}, HasLifecycle: true, LifecycleVersions: []string{"1.2"},
	}
	result := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"validator"}, Validators: []string{"syntax"}})
	pkg := result.Packages[0]
	if pkg.Action != FixPLCC || pkg.Catalog.Status() != "OK" || len(pkg.Issues) != 0 || len(pkg.Failures) == 0 {
		t.Errorf("full catalog coverage hid PLCC failures: %+v", pkg)
	}
	delete(inventory.Packages, "validator")
	result = mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"validator"}, Validators: []string{"syntax"}})
	if result.Packages[0].Action != FixPLCC {
		t.Error("zero bundles hid PLCC failure")
	}
}

func TestAssessCompletenessAndRegression(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		bundles   []string
		lifecycle []string
		status    PLCCStatus
		coverage  string
		issues    []Issue
	}{
		{
			name: "covered required version absent from PLCC", pkg: "full",
			bundles: []string{"1.2.0", "1.3.1"}, lifecycle: []string{"1.2", "1.3"},
			status: PLCCRegressed, coverage: "OK",
			issues: []Issue{versionIssue(RegressedPLCCVersion, "1.3")},
		},
		{
			name: "uncovered required version absent from PLCC", pkg: "full",
			bundles: []string{"1.2.0", "1.3.1"}, lifecycle: []string{"1.2"},
			status: PLCCIncomplete, coverage: "1/2",
			issues: []Issue{versionIssue(MissingPLCCVersion, "1.3"), versionIssue(MissingCatalogLifecycleVersion, "1.3")},
		},
		{
			name: "all required versions absent from existing PLCC product", pkg: "full",
			bundles: []string{"2.0.0", "3.0.0"}, lifecycle: []string{},
			status: PLCCIncomplete, coverage: "0/2",
			issues: []Issue{
				versionIssue(MissingPLCCVersion, "2.0"), versionIssue(MissingCatalogLifecycleVersion, "2.0"),
				versionIssue(MissingPLCCVersion, "3.0"), versionIssue(MissingCatalogLifecycleVersion, "3.0"),
			},
		},
		{
			name: "lifecycle-only regressions are sorted and deduplicated", pkg: "full",
			bundles: []string{"1.2.0"}, lifecycle: []string{"1.2", "8.1", "2.0", "8.1"},
			status: PLCCRegressed, coverage: "OK",
			issues: []Issue{versionIssue(RegressedPLCCVersion, "2.0"), versionIssue(RegressedPLCCVersion, "8.1")},
		},
		{
			name: "regressions with no bundles", pkg: "full",
			lifecycle: []string{"1.2", "1.3", "2.1"}, status: PLCCRegressed, coverage: "NO BUNDLES",
			issues: []Issue{{Kind: MissingCatalogBundles}, versionIssue(RegressedPLCCVersion, "1.3"), versionIssue(RegressedPLCCVersion, "2.1")},
		},
		{
			name: "regressed and missing versions coexist", pkg: "full",
			bundles: []string{"1.2.0", "1.3.1", "1.3.2", "1.4.0"}, lifecycle: []string{"1.2", "1.3"},
			status: PLCCRegressed, coverage: "2/3",
			issues: []Issue{
				versionIssue(RegressedPLCCVersion, "1.3"), versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"),
			},
		},
		{
			name: "whole package absent with regression and incompleteness", pkg: "absent",
			bundles: []string{"1.3.0", "1.4.0"}, lifecycle: []string{"1.3", "2.0"},
			status: PLCCAbsent, coverage: "1/2",
			issues: []Issue{
				{Kind: MissingPLCCPackage}, versionIssue(RegressedPLCCVersion, "1.3"),
				versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"), versionIssue(RegressedPLCCVersion, "2.0"),
			},
		},
		{
			name: "known PLCC and lifecycle versions need no bundles", pkg: "full",
			bundles: []string{"1.2.0"}, lifecycle: []string{"1.2", "1.10"}, status: PLCCOK, coverage: "OK",
		},
		{
			name: "zero version is not a package-level issue", pkg: "full",
			bundles: []string{"0.0.0"}, lifecycle: []string{"0.0"}, status: PLCCRegressed, coverage: "OK",
			issues: []Issue{versionIssue(RegressedPLCCVersion, "0.0")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, _ := fixtures(t)
			catalogPkg := catalog.Package{HasLifecycle: true, LifecycleVersions: tt.lifecycle}
			for _, version := range tt.bundles {
				catalogPkg.Bundles = append(catalogPkg.Bundles, catalog.Bundle{Name: tt.pkg + ".v" + version, Version: version})
			}
			inventory := &catalog.Inventory{Packages: map[string]catalog.Package{tt.pkg: catalogPkg}}
			options := plcc.DatasetOptions{Packages: []string{tt.pkg}, Validators: []string{"syntax", "catalog"}}
			pkg := mustAssess(t, source, inventory, options).Packages[0]
			wantAction := FixPLCC
			switch tt.status {
			case PLCCOK:
				wantAction = OK
			case PLCCAbsent:
				wantAction = AddPLCC
			}
			if pkg.PLCC != tt.status || pkg.Action != wantAction || pkg.Catalog.Status() != tt.coverage || !reflect.DeepEqual(pkg.Issues, tt.issues) {
				t.Fatalf("assessment = %+v, want %s/%s/%s with issues %+v", pkg, tt.status, tt.coverage, wantAction, tt.issues)
			}
			// All-package reports must include the same evidence, including the
			// package present only in the catalog and the zero-bundle regression.
			options.Packages = nil
			all := mustAssess(t, source, inventory, options)
			if got := findPackage(t, all, tt.pkg); !reflect.DeepEqual(got, pkg) {
				t.Errorf("all-package assessment differs: %+v", got)
			}
		})
	}
}

func TestAssessStatusPrecedencePreservesFindings(t *testing.T) {
	source, _ := fixtures(t)
	source.Data[8].IsOperator = false // beta has a product failure as well as duplicate failures.
	for _, tt := range []struct {
		pkg    string
		status PLCCStatus
		labels []string
	}{
		{"converter", PLCCInvalid, []string{"REQ-DATE-03"}},
		{"beta", PLCCDuplicate, []string{"REQ-VAL-01", "REQ-VAL-01", "CUSTOM-01"}},
	} {
		t.Run(tt.pkg, func(t *testing.T) {
			inventory := &catalog.Inventory{Packages: map[string]catalog.Package{tt.pkg: {
				Bundles:      []catalog.Bundle{{Name: "present", Version: "1.2.0"}, {Name: "absent", Version: "1.4.0"}},
				HasLifecycle: true, LifecycleVersions: []string{"1.2", "2.0"},
			}}}
			pkg := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{tt.pkg}, Validators: []string{"syntax", "catalog"}}).Packages[0]
			wantIssues := []Issue{
				versionIssue(MissingPLCCVersion, "1.4"), versionIssue(MissingCatalogLifecycleVersion, "1.4"), versionIssue(RegressedPLCCVersion, "2.0"),
			}
			var labels []string
			for _, failure := range pkg.Failures {
				labels = append(labels, failure.Validator.Label)
			}
			if pkg.PLCC != tt.status || pkg.Action != FixPLCC || !slices.Equal(labels, tt.labels) || !reflect.DeepEqual(pkg.Issues, wantIssues) {
				t.Errorf("precedence hid a finding: %+v; labels %v", pkg, labels)
			}
		})
	}
}

func TestAssessSelectedPolicyAndRawVersionNames(t *testing.T) {
	source, inventory := fixtures(t)
	// This product is valid under daily syntax/catalog checks, but its modern
	// lifecycle requires a tier under the default semantic policy.
	product := &source.Data[3] // unpublished
	product.Versions[0].Phases[0].StartDate = "2025-01-01T00:00:00.000Z"
	product.Versions[0].Phases[0].EndDate = "2025-12-31T00:00:00.000Z"
	daily := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"unpublished"}, Validators: []string{"syntax", "catalog"}})
	if daily.Packages[0].Action != BuildOperator {
		t.Fatalf("daily policy unexpectedly rejected product: %+v", daily.Packages[0])
	}
	for _, rule := range daily.Validators {
		if rule.Group != "syntax" && rule.Group != "catalog" {
			t.Errorf("daily report claimed unselected validator: %+v", rule)
		}
	}
	defaults := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"unpublished"}})
	if defaults.Packages[0].Action != FixPLCC {
		t.Errorf("default semantic policy was not applied: %+v", defaults.Packages[0])
	}
	// PLCC names must match exactly: patch reduction applies only to bundles.
	product.Versions[0].Name = "1.2.3"
	raw := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"unpublished"}, Validators: []string{"none"}}).Packages[0]
	wantIssues := []Issue{{Kind: MissingCatalogLifecycle}, versionIssue(MissingPLCCVersion, "1.2"), versionIssue(MissingCatalogLifecycleVersion, "1.2")}
	if raw.Action != FixPLCC || !slices.Equal(raw.SourceVersions, []string{"1.2.3"}) || !reflect.DeepEqual(raw.Issues, wantIssues) {
		t.Errorf("invalid source version was normalized or lost: %+v", raw)
	}
}

func TestAssessErrors(t *testing.T) {
	source, inventory := fixtures(t)
	for _, options := range []plcc.DatasetOptions{{Validators: []string{"unknown"}}, {Validators: []string{"none", "syntax"}}} {
		if result, err := Assess(source, inventory, options); err == nil || result != nil {
			t.Errorf("invalid options returned %v, %v", result, err)
		}
	}
	if result, err := Assess(nil, inventory, plcc.DatasetOptions{}); err == nil || result != nil {
		t.Errorf("nil source returned %v, %v", result, err)
	}
	for _, bad := range []catalog.Package{
		{Bundles: []catalog.Bundle{{Name: "bad-bundle", Version: "nonsense"}}},
		{HasLifecycle: true, LifecycleVersions: []string{"1.2.3"}},
	} {
		inventory.Packages["z-invalid"] = bad
		if result, err := Assess(source, inventory, plcc.DatasetOptions{Validators: []string{"none"}}); err == nil || result != nil || !strings.Contains(err.Error(), "z-invalid") {
			t.Errorf("malformed catalog returned %v, %v", result, err)
		}
		// Version policy applies only to assessed packages.
		mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"full"}, Validators: []string{"none"}})
	}
}

func TestAssessOwnershipAndDeterminism(t *testing.T) {
	source, inventory := fixtures(t)
	sourceBefore, _ := json.Marshal(source)
	inventoryBefore, _ := json.Marshal(inventory)
	options := plcc.DatasetOptions{Validators: []string{"syntax", "catalog"}}
	first := mustAssess(t, source, inventory, options)
	second := mustAssess(t, source, inventory, options)
	assessmentBefore, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("assessment is not deterministic")
	}
	sourceAfter, _ := json.Marshal(source)
	inventoryAfter, _ := json.Marshal(inventory)
	if !bytes.Equal(sourceBefore, sourceAfter) || !bytes.Equal(inventoryBefore, inventoryAfter) {
		t.Fatal("assessment mutated its inputs")
	}
	for i := range source.Data {
		source.Data[i].Package = "changed"
		for j := range source.Data[i].Versions {
			source.Data[i].Versions[j].Name = "changed"
		}
	}
	for _, pkg := range inventory.Packages {
		for i := range pkg.Bundles {
			pkg.Bundles[i].Version = "changed"
		}
		for i := range pkg.LifecycleVersions {
			pkg.LifecycleVersions[i] = "changed"
		}
	}
	options.Validators[0] = "changed"
	assessmentAfter, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(assessmentBefore, assessmentAfter) {
		t.Error("assessment retained mutable input references")
	}
	// Issue versions must be owned by the assessment, including between runs.
	findPackage(t, first, "mixed").Issues[0].Version.Minor = 99
	if got := findPackage(t, second, "mixed").Issues[0].Version.Minor; got != 3 {
		t.Errorf("independent assessments share issue versions: got %d, want 3", got)
	}
	findPackage(t, first, "alpha").Failures[0].Reasons[0] = "changed"
	if findPackage(t, second, "alpha").Failures[0].Reasons[0] == "changed" {
		t.Error("independent assessments share failure data")
	}
}
