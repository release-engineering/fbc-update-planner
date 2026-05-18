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
)

func TestFilter(t *testing.T) {
	var callOrder []string
	f1 := func(p *Package) []string {
		callOrder = append(callOrder, "f1")
		return nil
	}
	f2 := func(p *Package) []string {
		callOrder = append(callOrder, "f2")
		return []string{"rejected"}
	}
	f3 := func(p *Package) []string {
		callOrder = append(callOrder, "f3")
		return nil
	}

	pkg := &Package{Name: "test"}
	reasons := pkg.Filter(f1, f2, f3)

	if len(reasons) != 1 || reasons[0] != "rejected" {
		t.Errorf("expected rejection from f2, got %v", reasons)
	}
	if len(callOrder) != 2 || callOrder[0] != "f1" || callOrder[1] != "f2" {
		t.Errorf("expected short-circuit after f2, call order = %v", callOrder)
	}
}

func TestFilterPointInTimePhases(t *testing.T) {
	tests := []struct {
		name   string
		pkg    Package
		wantOK bool
	}{
		{
			name: "aligned at beginning",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "GA", StartDate: "", EndDate: "2025-01-01"},
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
				},
			}}},
			wantOK: true,
		},
		{
			name: "aligned at end",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
					{Name: "EOL", StartDate: "2025-12-31", EndDate: ""},
				},
			}}},
			wantOK: true,
		},
		{
			name: "misaligned at beginning",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "GA", StartDate: "", EndDate: "2025-03-01"},
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
				},
			}}},
			wantOK: false,
		},
		{
			name: "misaligned at end",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
					{Name: "EOL", StartDate: "2026-03-01", EndDate: ""},
				},
			}}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := FilterPointInTimePhases(&tt.pkg)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

func TestFilterIncompletePhases(t *testing.T) {
	pkg := &Package{Versions: []Version{{
		Name: "1.0",
		Phases: []Phase{
			{Name: "GA", StartDate: "", EndDate: "2025-01-01"},
			{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
			{Name: "EOL", StartDate: "2025-12-31", EndDate: ""},
		},
	}}}

	reasons := FilterIncompletePhases(pkg)

	if len(reasons) != 0 {
		t.Errorf("expected no reasons, got %v", reasons)
	}
	if len(pkg.Versions[0].Phases) != 1 {
		t.Fatalf("expected 1 phase kept, got %d", len(pkg.Versions[0].Phases))
	}
	if pkg.Versions[0].Phases[0].Name != "Full support" {
		t.Errorf("expected 'Full support' kept, got %q", pkg.Versions[0].Phases[0].Name)
	}
}

func TestValidateHasVersions(t *testing.T) {
	if reasons := ValidateHasVersions(&Package{Name: "test"}); len(reasons) == 0 {
		t.Error("expected rejection for package with no versions")
	}
	if reasons := ValidateHasVersions(&Package{Versions: []Version{{Name: "1.0"}}}); len(reasons) != 0 {
		t.Errorf("unexpected rejection: %v", reasons)
	}
}

func TestValidateVersionNames(t *testing.T) {
	pkg := &Package{Versions: []Version{
		{Name: "4.12"},
		{Name: "4.12.1"},
		{Name: "latest"},
	}}
	reasons := ValidateVersionNames(pkg)
	if len(reasons) != 2 {
		t.Errorf("expected 2 invalid names, got %d: %v", len(reasons), reasons)
	}
}

func TestValidatePhases(t *testing.T) {
	tests := []struct {
		name   string
		pkg    Package
		wantOK bool
	}{
		{
			name: "valid continuous phases",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-06-30"},
					{Name: "Maintenance", StartDate: "2025-07-01", EndDate: "2025-12-31"},
				},
			}}},
			wantOK: true,
		},
		{
			name: "gap between phases",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-06-30"},
					{Name: "Maintenance", StartDate: "2025-08-01", EndDate: "2025-12-31"},
				},
			}}},
			wantOK: false,
		},
		{
			name: "end before begin",
			pkg: Package{Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{
					{Name: "Full support", StartDate: "2025-12-31", EndDate: "2025-01-01"},
				},
			}}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePhases(&tt.pkg)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

func TestValidateOCPCompatibility(t *testing.T) {
	pkg := &Package{Versions: []Version{{
		Name: "1.0",
		PlatformCompatibility: []Platform{{
			Name:     "openshift",
			Versions: []string{"4.12", "4.13.1", "latest"},
		}},
	}}}
	reasons := ValidateOCPCompatibility(pkg)
	if len(reasons) != 2 {
		t.Errorf("expected 2 invalid OCP versions, got %d: %v", len(reasons), reasons)
	}
}
