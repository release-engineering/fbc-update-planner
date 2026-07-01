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
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestNewPackage(t *testing.T) {
	product := plcc.Product{
		Name:    "Test Product",
		Package: "test-operator",
		Versions: []plcc.Version{
			{
				Name: "2.0",
				Phases: []plcc.Phase{
					{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
				},
				OpenShiftCompatibility: "4.12, 4.13",
			},
			{
				Name: "1.0",
				Phases: []plcc.Phase{
					{Name: "Full support", StartDate: "2024-01-01T00:00:00.000Z", EndDate: "2024-12-31T00:00:00.000Z"},
				},
			},
		},
	}

	pkg, err := newPackage(product)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pkg.Schema != Schema {
		t.Errorf("schema = %q, want %q", pkg.Schema, Schema)
	}
	if pkg.Name != "test-operator" {
		t.Errorf("name = %q, want %q", pkg.Name, "test-operator")
	}
	if len(pkg.Versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(pkg.Versions))
	}
	want10 := mustParseMajorMinor(t, "1.0")
	want20 := mustParseMajorMinor(t, "2.0")
	if pkg.Versions[0].Name != want10 || pkg.Versions[1].Name != want20 {
		t.Errorf("versions not sorted: got %s, %s", pkg.Versions[0].Name, pkg.Versions[1].Name)
	}
	if len(pkg.Versions[1].PlatformCompatibility) != 1 {
		t.Fatalf("expected 1 platform on v2.0, got %d", len(pkg.Versions[1].PlatformCompatibility))
	}
	ocp := pkg.Versions[1].PlatformCompatibility[0]
	if ocp.Name != "openshift" || len(ocp.Versions) != 2 {
		t.Errorf("OCP platform = %+v, want openshift with 2 versions", ocp)
	}
}

func TestNewPackageNATimestamp(t *testing.T) {
	product := plcc.Product{
		Package: "test",
		Versions: []plcc.Version{{
			Name: "1.0",
			Phases: []plcc.Phase{
				{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
			},
		}},
	}

	pkg, err := newPackage(product)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ph := pkg.Versions[0].Phases[0]
	if ph.StartDate != nil {
		t.Errorf("expected nil StartDate for N/A, got %v", ph.StartDate)
	}
	wantEnd := datePtr(t, "2025-01-01")
	if ph.EndDate == nil || ph.EndDate.String() != wantEnd.String() {
		t.Errorf("EndDate = %v, want %v", ph.EndDate, wantEnd)
	}
}

func TestNewPackageUnparseableTimestamp(t *testing.T) {
	product := plcc.Product{
		Package: "test",
		Versions: []plcc.Version{{
			Name: "1.0",
			Phases: []plcc.Phase{
				{Name: "GA", StartDate: "not-a-date", EndDate: "2025-01-01T00:00:00.000Z"},
			},
		}},
	}

	_, err := newPackage(product)
	if err == nil {
		t.Fatal("expected error for unparseable timestamp, got nil")
	}
}

func TestNewPackageEmptyTimestamp(t *testing.T) {
	product := plcc.Product{
		Package: "test",
		Versions: []plcc.Version{{
			Name: "1.0",
			Phases: []plcc.Phase{
				{Name: "GA", StartDate: "", EndDate: "2025-01-01T00:00:00.000Z"},
			},
		}},
	}

	pkg, err := newPackage(product)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ph := pkg.Versions[0].Phases[0]
	if ph.StartDate != nil {
		t.Errorf("expected nil StartDate for empty string, got %v", ph.StartDate)
	}
	wantEnd := datePtr(t, "2025-01-01")
	if ph.EndDate == nil || ph.EndDate.String() != wantEnd.String() {
		t.Errorf("EndDate = %v, want %v", ph.EndDate, wantEnd)
	}
}

func TestNewPackageInvalidVersionName(t *testing.T) {
	product := plcc.Product{
		Package: "test",
		Versions: []plcc.Version{{
			Name:   "not-semver",
			Phases: []plcc.Phase{{Name: "GA", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"}},
		}},
	}

	pkg, err := newPackage(product)
	if err == nil {
		t.Fatal("expected error for invalid version name, got nil")
	}
	if pkg != nil {
		t.Errorf("expected nil package on error, got %+v", pkg)
	}
}

func TestTranslateVersionOCPIgnored(t *testing.T) {
	tests := []struct {
		name   string
		compat string
	}{
		{"N/A", "N/A"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := plcc.Version{
				Name:                   "1.0",
				OpenShiftCompatibility: tt.compat,
			}
			fv, err := translateVersion(v)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(fv.PlatformCompatibility) != 0 {
				t.Errorf("expected no platform compatibility, got %d", len(fv.PlatformCompatibility))
			}
		})
	}
}

func TestTranslateVersionInvalidOCP(t *testing.T) {
	v := plcc.Version{
		Name:                   "1.0",
		OpenShiftCompatibility: "4.12, bad-version",
	}
	_, err := translateVersion(v)
	if err == nil {
		t.Fatal("expected error for invalid OCP compatibility version")
	}
}
