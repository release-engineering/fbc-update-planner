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
	"io"
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
	}

	valid, failures := Translate(products, DefaultFilters()...)

	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1; failures: %v", len(failures), failures)
	}
	if failures[0].PackageName != "bad-pkg" {
		t.Errorf("failures[0] package = %q, want %q", failures[0].PackageName, "bad-pkg")
	}
	if len(valid) != 1 {
		t.Fatalf("got %d valid packages, want 1", len(valid))
	}
	if valid[0].Name != "valid-pkg" {
		t.Errorf("valid[0] package name = %q, want %q", valid[0].Name, "valid-pkg")
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

	catalog.FilterPackages()
	catalog.FilterMilestonePhases()
	catalog.SortByPackage()

	var buf bytes.Buffer
	if _, err := GenerateFBC(catalog.Data, &buf, io.Discard, YAMLWriter{}); err != nil {
		t.Fatalf("generating FBC: %v", err)
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

	catalog.FilterPackages()
	catalog.FilterMilestonePhases()
	catalog.SortByPackage()

	var buf bytes.Buffer
	if _, err := GenerateFBC(catalog.Data, &buf, io.Discard, JSONPrettyWriter{}); err != nil {
		t.Fatalf("generating FBC: %v", err)
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

	catalog.FilterPackages()
	catalog.FilterMilestonePhases()
	catalog.SortByPackage()

	var buf bytes.Buffer
	if _, err := GenerateFBC(catalog.Data, &buf, io.Discard, JSONWriter{}); err != nil {
		t.Fatalf("generating FBC: %v", err)
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
