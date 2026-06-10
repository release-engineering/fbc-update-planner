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
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestTranslateAndValidate(t *testing.T) {
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
		{Name: "Dup A", Package: "dup-pkg", Versions: []plcc.Version{{Name: "1.0"}}},
		{Name: "Dup B", Package: "dup-pkg", Versions: []plcc.Version{{Name: "2.0"}}},
		{
			Name:    "Bad Version",
			Package: "bad-pkg",
			Versions: []plcc.Version{{
				Name:   "not-semver",
				Phases: []plcc.Phase{{Name: "GA", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"}},
			}},
		},
	}

	valid, failures := TranslateAndValidate(products, DefaultFilters()...)

	if len(valid) != 1 {
		t.Fatalf("got %d valid packages, want 1", len(valid))
	}
	if valid[0].Name != "valid-pkg" {
		t.Errorf("valid package name = %q, want %q", valid[0].Name, "valid-pkg")
	}

	if len(failures) != 2 {
		t.Fatalf("got %d failures, want 2", len(failures))
	}

	dupFound, badFound := false, false
	for _, f := range failures {
		if f.PackageName == "dup-pkg" {
			dupFound = true
		}
		if f.PackageName == "bad-pkg" {
			badFound = true
		}
		if f.Valid {
			t.Errorf("failure for %q has Valid=true, want false", f.PackageName)
		}
	}
	if !dupFound {
		t.Error("expected failure for duplicate package \"dup-pkg\"")
	}
	if !badFound {
		t.Error("expected failure for invalid version in \"bad-pkg\"")
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
