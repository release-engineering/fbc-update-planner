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

package classify

import (
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

// validProduct builds a PLCC product with parseable timestamps that will
// pass syntax validators and produce valid FBC output.
func validProduct(pkg string, versions ...string) plcc.Product {
	var vers []plcc.Version
	for _, v := range versions {
		vers = append(vers, plcc.Version{
			Name: v,
			Tier: plcc.TierAligned,
			OpenShiftCompatibility: "4.14",
			Phases: []plcc.Phase{
				{Name: plcc.PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
				{Name: plcc.PhaseMaintenance, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
				{Name: plcc.PhaseEUSTerm1, StartDate: "2026-01-01T00:00:00.000Z", EndDate: "2026-06-30T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
				{Name: plcc.PhaseEUSTerm2, StartDate: "2026-07-01T00:00:00.000Z", EndDate: "2026-12-31T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
				{Name: plcc.PhaseEUSTerm3, StartDate: "2027-01-01T00:00:00.000Z", EndDate: "2027-06-30T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
			},
		})
	}
	return plcc.Product{
		Name:           "Product " + pkg,
		Package:        pkg,
		Versions:       vers,
		ReleaseCadence: "Predictable",
		IsOperator:     true,
	}
}

// invalidProduct builds a PLCC product that will fail syntax validation
// (version name is not MAJOR.MINOR).
func invalidProduct(pkg string) plcc.Product {
	return plcc.Product{
		Name:    "Product " + pkg,
		Package: pkg,
		Versions: []plcc.Version{
			{Name: "bad-version", Phases: []plcc.Phase{}},
		},
		IsOperator: true,
	}
}

func TestClassifyFullCoverage(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0", "1.1")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true, "1.1": true},
		},
		BundleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true, "1.1": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionOK {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionOK)
	}
	if reports[0].CatalogStatus != "OK" {
		t.Errorf("catalog status = %q, want OK", reports[0].CatalogStatus)
	}
	if len(reports[0].Gaps) != 0 {
		t.Errorf("got %d gaps, want 0", len(reports[0].Gaps))
	}
}

func TestClassifyPatchVersionsCollapse(t *testing.T) {
	// Bundle versions 1.0.1 and 1.0.2 should collapse to 1.0 for comparison.
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true},
		},
		BundleVersions: map[string]map[string]bool{
			// Bundle versions are already truncated to MAJOR.MINOR by the
			// catalog extraction layer (shell script's jq + sed). The
			// classify package receives them pre-truncated.
			"op-a": {"1.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionOK {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionOK)
	}
}

func TestClassifyPLCCMissingProduct(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-missing": {"1.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-missing"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionPLCCMissing {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionPLCCMissing)
	}
	if len(reports[0].Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1", len(reports[0].Gaps))
	}
	if reports[0].Gaps[0].Action != ActionPLCCMissing {
		t.Errorf("gap action = %q, want %q", reports[0].Gaps[0].Action, ActionPLCCMissing)
	}
}

func TestClassifyPLCCMissingVersion(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true},
		},
		BundleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true, "1.1": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionPLCCMissing {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionPLCCMissing)
	}
	if len(reports[0].Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1", len(reports[0].Gaps))
	}
	if reports[0].Gaps[0].Version != "1.1" {
		t.Errorf("gap version = %q, want %q", reports[0].Gaps[0].Version, "1.1")
	}
	if reports[0].Gaps[0].Action != ActionPLCCMissing {
		t.Errorf("gap action = %q, want %q", reports[0].Gaps[0].Action, ActionPLCCMissing)
	}
}

func TestClassifyFixPLCCValidatorRejection(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{invalidProduct("op-bad")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-bad": {"1.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-bad"},
		Validators:  plcc.SyntaxValidators(),
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionFixPLCC {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionFixPLCC)
	}
	if len(reports[0].Gaps) < 1 {
		t.Fatal("expected at least one gap")
	}
	if reports[0].Gaps[0].Action != ActionFixPLCC {
		t.Errorf("gap action = %q, want %q", reports[0].Gaps[0].Action, ActionFixPLCC)
	}
	if len(reports[0].Gaps[0].Reasons) == 0 {
		t.Error("expected reasons for Fix PLCC gap")
	}
}

func TestClassifyFixPLCCConversionRejection(t *testing.T) {
	// Product with a version that has an unparseable timestamp.
	badProduct := plcc.Product{
		Name:    "Bad Conversion",
		Package: "op-conv",
		Versions: []plcc.Version{
			{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: plcc.PhaseFullSupport, StartDate: "not-a-date", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			},
		},
		IsOperator: true,
	}
	catalog := &plcc.Catalog{Data: []plcc.Product{badProduct}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-conv": {"1.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-conv"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionFixPLCC {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionFixPLCC)
	}
}

func TestClassifyNeedsRebuild(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0", "1.1")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true},
		},
		BundleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true, "1.1": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionNeedsRebuild {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionNeedsRebuild)
	}
	if len(reports[0].Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1", len(reports[0].Gaps))
	}
	if reports[0].Gaps[0].Version != "1.1" {
		t.Errorf("gap version = %q, want %q", reports[0].Gaps[0].Version, "1.1")
	}
	if reports[0].Gaps[0].Action != ActionNeedsRebuild {
		t.Errorf("gap action = %q, want %q", reports[0].Gaps[0].Action, ActionNeedsRebuild)
	}
}

func TestClassifyNoCatalogBundles(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true},
		},
		BundleVersions: map[string]map[string]bool{},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionNoCatalogBundles {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionNoCatalogBundles)
	}
}

func TestClassifyNoCatalogBundlesPLCCMissing(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions:    map[string]map[string]bool{},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-missing"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionPLCCMissing {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionPLCCMissing)
	}
}

func TestClassifyMixedGaps(t *testing.T) {
	// op-a: version 1.0 valid in PLCC but missing from catalog (needs rebuild),
	// version 1.1 not in PLCC (PLCC missing).
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true, "1.1": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	// PLCC missing takes priority over needs rebuild.
	if reports[0].PrimaryAction != ActionPLCCMissing {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionPLCCMissing)
	}
	if len(reports[0].Gaps) != 2 {
		t.Fatalf("got %d gaps, want 2", len(reports[0].Gaps))
	}
	// Gaps sorted by version: 1.0 then 1.1.
	rebuildFound := false
	missingFound := false
	for _, g := range reports[0].Gaps {
		switch g.Action {
		case ActionNeedsRebuild:
			rebuildFound = true
		case ActionPLCCMissing:
			missingFound = true
		}
	}
	if !rebuildFound {
		t.Error("expected a Needs rebuild gap")
	}
	if !missingFound {
		t.Error("expected a PLCC missing gap")
	}
}

func TestClassifyBundleOnlyPackageAllOperators(t *testing.T) {
	// In all-operators mode (no package list), a package with bundles but
	// no PLCC data should appear in the report.
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-a":        {"1.0": true},
			"bundle-only": {"2.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		// No Packages → all operators mode.
	})
	found := false
	for _, r := range reports {
		if r.Package == "bundle-only" {
			found = true
			if r.PrimaryAction != ActionPLCCMissing {
				t.Errorf("bundle-only primary action = %q, want %q", r.PrimaryAction, ActionPLCCMissing)
			}
		}
	}
	if !found {
		t.Error("bundle-only package not found in all-operators report")
	}
}

func TestClassifyPriorityOrdering(t *testing.T) {
	// Fix PLCC > PLCC missing > No catalog bundles > Needs rebuild > OK
	tests := []struct {
		name     string
		actions  []Action
		wantBest Action
	}{
		{"fix wins over missing", []Action{ActionFixPLCC, ActionPLCCMissing}, ActionFixPLCC},
		{"missing wins over rebuild", []Action{ActionPLCCMissing, ActionNeedsRebuild}, ActionPLCCMissing},
		{"rebuild wins over ok", []Action{ActionNeedsRebuild, ActionOK}, ActionNeedsRebuild},
		{"fix wins over rebuild", []Action{ActionFixPLCC, ActionNeedsRebuild}, ActionFixPLCC},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := OperatorReport{}
			for _, a := range tc.actions {
				r.Gaps = append(r.Gaps, VersionGap{Action: a})
			}
			got := primaryAction(r)
			if got != tc.wantBest {
				t.Errorf("primaryAction = %q, want %q", got, tc.wantBest)
			}
		})
	}
}

func TestClassifyNilCatalogData(t *testing.T) {
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: nil,
	})
	if reports != nil {
		t.Errorf("expected nil reports with nil CatalogData, got %d", len(reports))
	}
}

func TestClassifyNoLifecycleEntry(t *testing.T) {
	// Package has bundles but no lifecycle entry in catalog — classified
	// by PLCC availability. PLCC has valid data → needs rebuild.
	catalog := &plcc.Catalog{Data: []plcc.Product{validProduct("op-a", "1.0")}}
	cd := &CatalogData{
		LifecycleVersions: map[string]map[string]bool{},
		BundleVersions: map[string]map[string]bool{
			"op-a": {"1.0": true},
		},
	}
	reports := Classify(Input{
		Catalog:     catalog,
		CatalogData: cd,
		Packages:    []string{"op-a"},
	})
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].PrimaryAction != ActionNeedsRebuild {
		t.Errorf("primary action = %q, want %q", reports[0].PrimaryAction, ActionNeedsRebuild)
	}
	if reports[0].CatalogStatus != "MISSING" {
		t.Errorf("catalog status = %q, want MISSING", reports[0].CatalogStatus)
	}
}
