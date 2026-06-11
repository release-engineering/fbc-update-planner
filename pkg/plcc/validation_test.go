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

package plcc

import "testing"

func TestValidateProduct(t *testing.T) {
	always := func(p Product) []string { return []string{"always"} }
	never := func(p Product) []string { return nil }

	if reasons := ValidateProduct(Product{}, never, never); len(reasons) != 0 {
		t.Errorf("expected no reasons, got %v", reasons)
	}
	if reasons := ValidateProduct(Product{}, never, always); len(reasons) != 1 {
		t.Errorf("expected 1 reason, got %v", reasons)
	}
	if reasons := ValidateProduct(Product{}, always, always); len(reasons) != 2 {
		t.Errorf("expected 2 reasons, got %v", reasons)
	}
}

func TestCatalogValidate(t *testing.T) {
	catalog := &Catalog{Data: []Product{
		{Package: "dup-pkg"}, {Package: "dup-pkg"}, {Package: "unique-pkg"},
	}}
	reasons := catalog.Validate(ValidateNoDuplicates)
	if len(reasons) != 1 {
		t.Errorf("expected 1 reason for duplicate, got %d: %v", len(reasons), reasons)
	}
}

// --- REQ-DATE-02 ---

func TestValidateDatesStatic(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"format=date", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z", StartDateFormat: "date", EndDateFormat: "date"},
		}}}}, true},
		{"N/A with format=string skipped", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z", StartDateFormat: "string", EndDateFormat: "date"},
		}}}}, true},
		{"empty with format=string skipped", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "", EndDate: "2025-01-01T00:00:00.000Z", StartDateFormat: "string", EndDateFormat: "date"},
		}}}}, true},
		{"free-form start with format=string", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "Release of 1.3", EndDate: "2025-06-30T00:00:00.000Z", StartDateFormat: "string", EndDateFormat: "date"},
		}}}}, false},
		{"free-form end with format=string", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "End of OCP 4.12", StartDateFormat: "date", EndDateFormat: "string"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateDatesStatic(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-DATE-03 ---

func TestValidateDatesClean(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"clean dates", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, true},
		{"N/A skipped", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
		}}}}, true},
		{"dirty date", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "not-a-date", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateDatesClean(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-DATE-04 ---

func TestValidateDatesContiguity(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"contiguous", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: "Maintenance", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
		}}}}, true},
		{"same-day transition", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: "Maintenance", StartDate: "2025-06-30T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
		}}}}, false},
		{"multi-day gap", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: "Maintenance", StartDate: "2025-07-05T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
		}}}}, false},
		{"N/A phases skipped", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateDatesContiguity(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-VER-01 ---

func TestValidateVersionNames(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"valid MAJOR.MINOR", Product{Versions: []Version{{Name: "1.0"}, {Name: "4.12"}}}, true},
		{"invalid patch", Product{Versions: []Version{{Name: "1.0.1"}}}, false},
		{"invalid suffix", Product{Versions: []Version{{Name: "4.6 EUS"}}}, false},
		{"invalid wildcard", Product{Versions: []Version{{Name: "6.x"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateVersionNames(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-PA-01 ---

func TestValidatePlatformAlignedPhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"all phases with dates", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseMaintenance, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
			{Name: PhaseEUSTerm1, StartDate: "2026-01-01T00:00:00.000Z", EndDate: "2026-06-30T00:00:00.000Z"},
			{Name: PhaseEUSTerm2, StartDate: "2026-07-01T00:00:00.000Z", EndDate: "2026-12-31T00:00:00.000Z"},
			{Name: PhaseEUSTerm3, StartDate: "2027-01-01T00:00:00.000Z", EndDate: "2027-06-30T00:00:00.000Z"},
		}}}}, true},
		{"non-aligned skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling"}}}, true},
		{"phase present but N/A dates", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseMaintenance, StartDate: "N/A", EndDate: "N/A"},
			{Name: PhaseEUSTerm1, StartDate: "N/A", EndDate: "N/A"},
			{Name: PhaseEUSTerm2, StartDate: "N/A", EndDate: "N/A"},
			{Name: PhaseEUSTerm3, StartDate: "N/A", EndDate: "N/A"},
		}}}}, false},
		{"missing EUS term 2", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseMaintenance, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
			{Name: PhaseEUSTerm1, StartDate: "2026-01-01T00:00:00.000Z", EndDate: "2026-06-30T00:00:00.000Z"},
			{Name: PhaseEUSTerm3, StartDate: "2027-01-01T00:00:00.000Z", EndDate: "2027-06-30T00:00:00.000Z"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePlatformAlignedPhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-PA-02 ---

func TestValidatePlatformAlignedOCP(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"OCP present", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "4.16"}}}, true},
		{"non-aligned skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling"}}}, true},
		{"missing OCP", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned"}}}, false},
		{"OCP is N/A", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "N/A"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePlatformAlignedOCP(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-AG-01 ---

func TestValidatePlatformAgnosticPhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"phases with dates", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseMaintenance, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
		}}}}, true},
		{"non-agnostic skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling"}}}, true},
		{"phase present but N/A dates", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseMaintenance, StartDate: "N/A", EndDate: "N/A"},
		}}}}, false},
		{"missing maintenance", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePlatformAgnosticPhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-AG-03 ---

func TestValidatePlatformAgnosticEUSPhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"not EUS-aligned skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseFullSupport},
		}}}}, true},
		{"all EUS terms with dates", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseEUSTerm1, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseEUSTerm2, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
			{Name: PhaseEUSTerm3, StartDate: "2026-01-01T00:00:00.000Z", EndDate: "2026-06-30T00:00:00.000Z"},
		}}}}, true},
		{"missing term 3", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseEUSTerm1, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseEUSTerm2, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
		}}}}, false},
		{"term 3 present but N/A dates", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseEUSTerm1, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: PhaseEUSTerm2, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
			{Name: PhaseEUSTerm3, StartDate: "N/A", EndDate: "N/A"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePlatformAgnosticEUSPhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-AG-04 ---

func TestValidatePlatformAgnosticEUSOCP(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"not EUS-aligned skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseFullSupport},
		}}}}, true},
		{"EUS-aligned with OCP", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseEUSTerm1, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}, OpenShiftCompatibility: "4.14"}}}, true},
		{"EUS-aligned missing OCP", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", Phases: []Phase{
			{Name: PhaseEUSTerm1, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePlatformAgnosticEUSOCP(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-RS-01 ---

func TestValidateRollingStreamPhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"full support with dates", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, true},
		{"non-rolling skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Aligned"}}}, true},
		{"missing full support", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseGA},
		}}}}, false},
		{"full support with N/A dates", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseFullSupport, StartDate: "N/A", EndDate: "N/A"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateRollingStreamPhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-RS-02 ---

func TestValidateRollingStreamForbiddenPhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"no forbidden phases", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseFullSupport},
		}}}}, true},
		{"non-rolling skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Aligned"}}}, true},
		{"has maintenance", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseFullSupport}, {Name: PhaseMaintenance},
		}}}}, false},
		{"has EUS phase", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", Phases: []Phase{
			{Name: PhaseFullSupport}, {Name: PhaseEUSTerm1},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateRollingStreamForbiddenPhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-ALL-01 ---

func TestValidateReleaseCadence(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"operator with cadence", Product{IsOperator: true, ReleaseCadence: "4 months"}, true},
		{"non-operator skipped", Product{IsOperator: false, ReleaseCadence: ""}, true},
		{"operator empty cadence", Product{IsOperator: true, ReleaseCadence: ""}, false},
		{"operator Not Specified", Product{IsOperator: true, ReleaseCadence: "Not Specified"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateReleaseCadence(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-TIER-ALL-02 ---

func TestValidateTierSelected(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"operator with Aligned", Product{IsOperator: true, Versions: []Version{{Name: "1.0", Tier: "Aligned"}}}, true},
		{"non-operator skipped", Product{IsOperator: false, Versions: []Version{{Name: "1.0", Tier: "N/A"}}}, true},
		{"operator N/A tier", Product{IsOperator: true, Versions: []Version{{Name: "1.0", Tier: "N/A"}}}, false},
		{"operator dash tier", Product{IsOperator: true, Versions: []Version{{Name: "1.0", Tier: "-"}}}, false},
		{"operator empty tier", Product{IsOperator: true, Versions: []Version{{Name: "1.0", Tier: ""}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateTierSelected(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-FIELD-02 ---

func TestValidateOCPFormat(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"valid format", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "4.12, 4.13"}}}, true},
		{"non-aligned skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling", OpenShiftCompatibility: "bad"}}}, true},
		{"N/A skipped", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "N/A"}}}, true},
		{"invalid format", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "4.6 EUS"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateOCPFormat(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- REQ-VAL-01 ---

func TestValidateNoDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		products []Product
		wantOK   bool
	}{
		{"no duplicates", []Product{{Package: "a"}, {Package: "b"}}, true},
		{"has duplicate", []Product{{Package: "a"}, {Package: "a"}}, false},
		{"empty packages ignored", []Product{{Package: ""}, {Package: ""}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateNoDuplicates(tt.products)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- CUSTOM-01 ---

func TestValidateIsOperator(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"has package and is_operator", Product{Package: "my-operator", IsOperator: true}, true},
		{"missing package", Product{Package: "", IsOperator: true}, false},
		{"not operator", Product{Package: "my-pkg", IsOperator: false}, false},
		{"both missing", Product{Package: "", IsOperator: false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateIsOperator(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- CUSTOM-02 ---

func TestValidateHasVersions(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"has versions", Product{Versions: []Version{{Name: "1.0"}}}, true},
		{"no versions", Product{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateHasVersions(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- CUSTOM-03 ---

func TestValidatePointInTimePhases(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"aligned before first (+1 day rule)", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2024-12-31T00:00:00.000Z"},
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, true},
		{"aligned after last (+1 day rule)", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: "EOL", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "N/A"},
		}}}}, true},
		{"misaligned before first", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2025-01-02T00:00:00.000Z"},
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, false},
		{"misaligned after last", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
			{Name: "EOL", StartDate: "2025-07-05T00:00:00.000Z", EndDate: "N/A"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePointInTimePhases(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- CUSTOM-04 ---

func TestValidatePhaseEndAfterStart(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"end after start", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		}}}}, true},
		{"N/A dates skipped", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
		}}}}, true},
		{"end before start", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-06-30T00:00:00.000Z", EndDate: "2025-01-01T00:00:00.000Z"},
		}}}}, false},
		{"same day", Product{Versions: []Version{{Name: "1.0", Phases: []Phase{
			{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-01-01T00:00:00.000Z"},
		}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidatePhaseEndAfterStart(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}

// --- CUSTOM-05 ---

func TestValidateOCPFormatAll(t *testing.T) {
	tests := []struct {
		name   string
		p      Product
		wantOK bool
	}{
		{"valid on non-aligned", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", OpenShiftCompatibility: "4.12"}}}, true},
		{"aligned skipped", Product{Versions: []Version{{Name: "4.16", Tier: "Aligned", OpenShiftCompatibility: "bad"}}}, true},
		{"invalid on non-aligned", Product{Versions: []Version{{Name: "1.0", Tier: "Agnostic", OpenShiftCompatibility: "4.6 EUS"}}}, false},
		{"empty skipped", Product{Versions: []Version{{Name: "1.0", Tier: "Rolling"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := ValidateOCPFormatAll(tt.p)
			if (len(reasons) == 0) != tt.wantOK {
				t.Errorf("ok = %v, want %v; reasons: %v", len(reasons) == 0, tt.wantOK, reasons)
			}
		})
	}
}
