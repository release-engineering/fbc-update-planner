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

func TestDefaultFiltersFromRegistry(t *testing.T) {
	filters := DefaultFilters()
	if len(filters) != 1 {
		t.Errorf("got %d filters, want 1", len(filters))
	}
}

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

func TestValidatePackageHasVersions(t *testing.T) {
	tests := []struct {
		name       string
		pkg        *Package
		wantReject bool
	}{
		{
			name:       "no versions",
			pkg:        &Package{Name: "test"},
			wantReject: true,
		},
		{
			name: "has versions",
			pkg: &Package{Name: "test", Versions: []Version{{
				Name:   mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-12-31")}},
			}}},
			wantReject: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePackageHasVersions(tt.pkg)
			if tt.wantReject && len(reasons) == 0 {
				t.Error("expected rejection, got none")
			}
			if !tt.wantReject && len(reasons) > 0 {
				t.Errorf("expected no rejection, got %v", reasons)
			}
		})
	}
}

func TestValidateVersionsHavePhases(t *testing.T) {
	tests := []struct {
		name       string
		pkg        *Package
		wantReject bool
	}{
		{
			name: "version with no phases",
			pkg: &Package{Versions: []Version{
				{Name: mustParseMajorMinor(t, "1.0")},
			}},
			wantReject: true,
		},
		{
			name: "all versions have phases",
			pkg: &Package{Versions: []Version{{
				Name:   mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-12-31")}},
			}}},
			wantReject: false,
		},
		{
			name: "one version missing phases",
			pkg: &Package{Versions: []Version{
				{Name: mustParseMajorMinor(t, "1.0"), Phases: []Phase{{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-12-31")}}},
				{Name: mustParseMajorMinor(t, "2.0")},
			}},
			wantReject: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateVersionsHavePhases(tt.pkg)
			if tt.wantReject && len(reasons) == 0 {
				t.Error("expected rejection, got none")
			}
			if !tt.wantReject && len(reasons) > 0 {
				t.Errorf("expected no rejection, got %v", reasons)
			}
		})
	}
}

func TestValidatePhaseDates(t *testing.T) {
	tests := []struct {
		name       string
		pkg        *Package
		wantReject bool
		wantCount  int
	}{
		{
			name: "all dates present",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
				},
			}}},
			wantReject: false,
		},
		{
			name: "zero start date",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", EndDate: mustParseDate(t, "2025-06-30")},
				},
			}}},
			wantReject: true,
			wantCount:  1,
		},
		{
			name: "zero end date",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t, "2025-01-01")},
				},
			}}},
			wantReject: true,
			wantCount:  1,
		},
		{
			name: "both dates zero",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA"},
				},
			}}},
			wantReject: true,
			wantCount:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePhaseDates(tt.pkg)
			if tt.wantReject && len(reasons) == 0 {
				t.Error("expected rejection, got none")
			}
			if !tt.wantReject && len(reasons) > 0 {
				t.Errorf("expected no rejection, got %v", reasons)
			}
			if tt.wantReject && len(reasons) != tt.wantCount {
				t.Errorf("expected %d reasons, got %d: %v", tt.wantCount, len(reasons), reasons)
			}
		})
	}
}

func TestValidateDateOrdering(t *testing.T) {
	tests := []struct {
		name       string
		pkg        *Package
		wantReject bool
	}{
		{
			name: "start before end",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
				},
			}}},
			wantReject: false,
		},
		{
			name: "start equals end",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-01-01")},
				},
			}}},
			wantReject: false,
		},
		{
			name: "start after end",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-06-30"), EndDate: mustParseDate(t,"2025-01-01")},
				},
			}}},
			wantReject: true,
		},
		{
			name: "multiple phases one invalid",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
					{Name: "Maintenance", StartDate: mustParseDate(t,"2025-12-31"), EndDate: mustParseDate(t,"2025-07-01")},
				},
			}}},
			wantReject: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateDateOrdering(tt.pkg)
			if tt.wantReject && len(reasons) == 0 {
				t.Error("expected rejection, got none")
			}
			if !tt.wantReject && len(reasons) > 0 {
				t.Errorf("expected no rejection, got %v", reasons)
			}
		})
	}
}

func TestValidatePhaseContiguity(t *testing.T) {
	tests := []struct {
		name       string
		pkg        *Package
		wantReject bool
	}{
		{
			name: "contiguous phases",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
					{Name: "Maintenance", StartDate: mustParseDate(t,"2025-07-01"), EndDate: mustParseDate(t,"2025-12-31")},
					{Name: "EOL", StartDate: mustParseDate(t,"2026-01-01"), EndDate: mustParseDate(t,"2026-06-30")},
				},
			}}},
			wantReject: false,
		},
		{
			name: "one day gap between phases",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
					{Name: "Maintenance", StartDate: mustParseDate(t,"2025-07-02"), EndDate: mustParseDate(t,"2025-12-31")},
				},
			}}},
			wantReject: true,
		},
		{
			name: "one day overlap between phases",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-07-01")},
					{Name: "Maintenance", StartDate: mustParseDate(t,"2025-07-01"), EndDate: mustParseDate(t,"2025-12-31")},
				},
			}}},
			wantReject: true,
		},
		{
			name: "single phase",
			pkg: &Package{Versions: []Version{{
				Name: mustParseMajorMinor(t, "1.0"),
				Phases: []Phase{
					{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-12-31")},
				},
			}}},
			wantReject: false,
		},
		{
			name: "multiple versions all contiguous",
			pkg: &Package{Versions: []Version{
				{
					Name: mustParseMajorMinor(t, "1.0"),
					Phases: []Phase{
						{Name: "GA", StartDate: mustParseDate(t,"2025-01-01"), EndDate: mustParseDate(t,"2025-06-30")},
						{Name: "EOL", StartDate: mustParseDate(t,"2025-07-01"), EndDate: mustParseDate(t,"2025-12-31")},
					},
				},
				{
					Name: mustParseMajorMinor(t, "2.0"),
					Phases: []Phase{
						{Name: "GA", StartDate: mustParseDate(t,"2025-06-01"), EndDate: mustParseDate(t,"2025-11-30")},
						{Name: "EOL", StartDate: mustParseDate(t,"2025-12-01"), EndDate: mustParseDate(t,"2026-05-31")},
					},
				},
			}},
			wantReject: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePhaseContiguity(tt.pkg)
			if tt.wantReject && len(reasons) == 0 {
				t.Error("expected rejection, got none")
			}
			if !tt.wantReject && len(reasons) > 0 {
				t.Errorf("expected no rejection, got %v", reasons)
			}
		})
	}
}
