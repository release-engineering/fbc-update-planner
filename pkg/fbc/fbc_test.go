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

	pkg := NewPackage(product)

	if pkg.Schema != Schema {
		t.Errorf("schema = %q, want %q", pkg.Schema, Schema)
	}
	if pkg.Name != "test-operator" {
		t.Errorf("name = %q, want %q", pkg.Name, "test-operator")
	}
	if len(pkg.Versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(pkg.Versions))
	}
	if pkg.Versions[0].Name != "1.0" || pkg.Versions[1].Name != "2.0" {
		t.Errorf("versions not sorted: got %q, %q", pkg.Versions[0].Name, pkg.Versions[1].Name)
	}
	if len(pkg.Versions[1].PlatformCompatibility) != 1 {
		t.Fatalf("expected 1 platform on v2.0, got %d", len(pkg.Versions[1].PlatformCompatibility))
	}
	ocp := pkg.Versions[1].PlatformCompatibility[0]
	if ocp.Name != "openshift" || len(ocp.Versions) != 2 {
		t.Errorf("OCP platform = %+v, want openshift with 2 versions", ocp)
	}
}

func TestNewPackageUnparseableTimestamp(t *testing.T) {
	product := plcc.Product{
		Package: "test",
		Versions: []plcc.Version{{
			Name: "1.0",
			Phases: []plcc.Phase{
				{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
			},
		}},
	}

	pkg := NewPackage(product)
	ph := pkg.Versions[0].Phases[0]

	if ph.StartDate != "" {
		t.Errorf("expected empty StartDate for N/A, got %q", ph.StartDate)
	}
	if ph.EndDate != "2025-01-01" {
		t.Errorf("EndDate = %q, want %q", ph.EndDate, "2025-01-01")
	}
}

func TestTranslateVersionOCPIgnored(t *testing.T) {
	tests := []struct {
		name  string
		compat string
	}{
		{"N/A", "N/A"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := plcc.Version{Name: "1.0", OpenShiftCompatibility: tt.compat}
			fv := translateVersion(v)
			if len(fv.PlatformCompatibility) != 0 {
				t.Errorf("expected no platform compatibility, got %d", len(fv.PlatformCompatibility))
			}
		})
	}
}

func TestCompareMajorMinor(t *testing.T) {
	tests := []struct {
		a, b     string
		wantSign int
	}{
		{"1.0", "2.0", -1},
		{"2.0", "1.0", 1},
		{"4.12", "4.13", -1},
		{"4.12", "4.12", 0},
	}
	for _, tt := range tests {
		got := compareMajorMinor(tt.a, tt.b)
		switch {
		case tt.wantSign < 0 && got >= 0:
			t.Errorf("compareMajorMinor(%q, %q) = %d, want < 0", tt.a, tt.b, got)
		case tt.wantSign > 0 && got <= 0:
			t.Errorf("compareMajorMinor(%q, %q) = %d, want > 0", tt.a, tt.b, got)
		case tt.wantSign == 0 && got != 0:
			t.Errorf("compareMajorMinor(%q, %q) = %d, want 0", tt.a, tt.b, got)
		}
	}
}
