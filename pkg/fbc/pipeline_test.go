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

package fbc

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestTranslate(t *testing.T) {
	products := []plcc.Product{
		{
			Name:    "Valid Product",
			Package: "valid-pkg",
			Versions: []plcc.Version{{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
					{Name: "Maintenance", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			}},
		},
		{
			Name:    "Bad Version",
			Package: "bad-pkg",
			Versions: []plcc.Version{{
				Name:   "not-semver",
				Phases: []plcc.Phase{{Name: "GA", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"}},
			}},
		},
		{
			Name:    "Unparseable Timestamp",
			Package: "bad-timestamp-pkg",
			Versions: []plcc.Version{{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			}},
		},
		{
			Name:    "Empty Dates",
			Package: "empty-dates-pkg",
			Versions: []plcc.Version{{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "GA", StartDate: "", EndDate: "2025-01-01T00:00:00.000Z"},
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			}},
		},
		{
			Name:    "Multi Package Product",
			Package: "alpha-op",
			Versions: []plcc.Version{{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
					{Name: "Maintenance", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			}},
		},
		{
			Name:    "Multi Package Product",
			Package: "beta-op",
			Versions: []plcc.Version{{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
					{Name: "Maintenance", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
				},
			}},
		},
	}

	valid, failures := Translate(products, DefaultFilters()...)

	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1; failures: %v", len(failures), failures)
	}
	if failures[0].PackageName != "bad-pkg" {
		t.Errorf("failures[0] package = %q, want %q", failures[0].PackageName, "bad-pkg")
	}
	if len(valid) != 5 {
		t.Fatalf("got %d valid packages, want 5", len(valid))
	}
	if valid[0].Name != "valid-pkg" {
		t.Errorf("valid[0] package name = %q, want %q", valid[0].Name, "valid-pkg")
	}
	// bad-timestamp-pkg: N/A start date becomes nil, GA phase stripped by FilterIncompletePhases
	if valid[1].Name != "bad-timestamp-pkg" {
		t.Errorf("valid[1] package name = %q, want %q", valid[1].Name, "bad-timestamp-pkg")
	}
	if len(valid[1].Versions[0].Phases) != 1 {
		t.Errorf("bad-timestamp-pkg: got %d phases, want 1 (GA should be stripped)", len(valid[1].Versions[0].Phases))
	}
	// empty-dates-pkg: empty start date becomes nil, GA phase stripped by FilterIncompletePhases
	if valid[2].Name != "empty-dates-pkg" {
		t.Errorf("valid[2] package name = %q, want %q", valid[2].Name, "empty-dates-pkg")
	}
	if len(valid[2].Versions[0].Phases) != 1 {
		t.Errorf("empty-dates-pkg: got %d phases, want 1 (GA should be stripped)", len(valid[2].Versions[0].Phases))
	}
	if valid[3].Name != "alpha-op" {
		t.Errorf("valid[3] package name = %q, want %q", valid[3].Name, "alpha-op")
	}
	if valid[4].Name != "beta-op" {
		t.Errorf("valid[4] package name = %q, want %q", valid[4].Name, "beta-op")
	}
}

func TestTranslateProduct(t *testing.T) {
	tests := []struct {
		name       string
		product    plcc.Product
		wantPkg    bool
		wantReason string
	}{
		{
			name: "valid product",
			product: plcc.Product{
				Package: "test-pkg",
				Versions: []plcc.Version{{
					Name: "1.0",
					Phases: []plcc.Phase{
						{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
					},
				}},
			},
			wantPkg: true,
		},
		{
			name: "invalid version name",
			product: plcc.Product{
				Package: "bad-pkg",
				Versions: []plcc.Version{{
					Name:   "not-semver",
					Phases: []plcc.Phase{{Name: "GA", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"}},
				}},
			},
			wantReason: "invalid version",
		},
		{
			name: "N/A timestamps filtered",
			product: plcc.Product{
				Package: "na-pkg",
				Versions: []plcc.Version{{
					Name: "1.0",
					Phases: []plcc.Phase{
						{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
						{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
					},
				}},
			},
			wantPkg: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, failure := TranslateProduct(tt.product, DefaultFilters()...)
			if tt.wantPkg {
				if pkg == nil {
					t.Fatal("expected package, got nil")
				}
				if failure != nil {
					t.Fatalf("expected no failure, got %v", failure.Reasons)
				}
				return
			}
			if pkg != nil {
				t.Fatalf("expected nil package, got %+v", pkg)
			}
			if failure == nil {
				t.Fatal("expected failure, got nil")
			}
			if failure.PackageName != tt.product.Package {
				t.Errorf("failure.PackageName = %q, want %q", failure.PackageName, tt.product.Package)
			}
			if tt.wantReason != "" {
				found := false
				for _, r := range failure.Reasons {
					if strings.Contains(r, tt.wantReason) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected reason containing %q, got %v", tt.wantReason, failure.Reasons)
				}
			}
		})
	}
}

// TestReferenceFile runs the full pipeline on the reference PLCC testdata/plcc.json file.
// The result is compared against the expected FBC file output (testdata/reference-fbc.yaml).
func TestReferenceFile(t *testing.T) {
	catalog, err := plcc.Load("testdata/plcc.json")
	if err != nil {
		t.Fatalf("loading PLCC test data: %v", err)
	}

	catalog.DropWithoutPackageName()
	catalog.ExpandPackages()
	catalog.SortByPackage()

	var buf bytes.Buffer
	valid, _ := Translate(catalog.Data, DefaultFilters()...)
	if err := (YAMLWriter{}).Write(&buf, valid...); err != nil {
		t.Fatalf("writing FBC: %v", err)
	}

	want, err := os.ReadFile("testdata/reference-fbc.yaml")
	if err != nil {
		t.Fatalf("reading reference file: %v", err)
	}

	if buf.String() != string(want) {
		t.Errorf("FBC output does not match reference file (got %d bytes, want %d bytes)", buf.Len(), len(want))
		if err := os.WriteFile("testdata/actual-fbc.yaml", buf.Bytes(), 0644); err != nil {
			t.Logf("failed to write actual output: %v", err)
		}
		t.Log("actual output written to testdata/actual-fbc.yaml")
	}
}

func TestReferenceFileJSONPretty(t *testing.T) {
	catalog, err := plcc.Load("testdata/plcc.json")
	if err != nil {
		t.Fatalf("loading PLCC test data: %v", err)
	}

	catalog.DropWithoutPackageName()
	catalog.ExpandPackages()
	catalog.SortByPackage()

	var buf bytes.Buffer
	valid, _ := Translate(catalog.Data, DefaultFilters()...)
	if err := (JSONPrettyWriter{}).Write(&buf, valid...); err != nil {
		t.Fatalf("writing FBC: %v", err)
	}

	want, err := os.ReadFile("testdata/reference-fbc-pretty.json")
	if err != nil {
		t.Fatalf("reading reference file: %v", err)
	}

	if buf.String() != string(want) {
		t.Errorf("FBC JSON pretty output does not match reference file (got %d bytes, want %d bytes)", buf.Len(), len(want))
		if err := os.WriteFile("testdata/actual-fbc-pretty.json", buf.Bytes(), 0644); err != nil {
			t.Logf("failed to write actual output: %v", err)
		}
		t.Log("actual output written to testdata/actual-fbc-pretty.json")
	}
}

func TestReferenceFileJSON(t *testing.T) {
	catalog, err := plcc.Load("testdata/plcc.json")
	if err != nil {
		t.Fatalf("loading PLCC test data: %v", err)
	}

	catalog.DropWithoutPackageName()
	catalog.ExpandPackages()
	catalog.SortByPackage()

	var buf bytes.Buffer
	valid, _ := Translate(catalog.Data, DefaultFilters()...)
	if err := (JSONWriter{}).Write(&buf, valid...); err != nil {
		t.Fatalf("writing FBC: %v", err)
	}

	want, err := os.ReadFile("testdata/reference-fbc.json")
	if err != nil {
		t.Fatalf("reading reference file: %v", err)
	}

	if buf.String() != string(want) {
		t.Errorf("FBC JSON output does not match reference file (got %d bytes, want %d bytes)", buf.Len(), len(want))
		if err := os.WriteFile("testdata/actual-fbc.json", buf.Bytes(), 0644); err != nil {
			t.Logf("failed to write actual output: %v", err)
		}
		t.Log("actual output written to testdata/actual-fbc.json")
	}
}
