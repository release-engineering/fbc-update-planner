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

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func mustDataset(t *testing.T, source *Catalog, options DatasetOptions) *Dataset {
	t.Helper()
	dataset, err := NewDataset(source, options)
	if err != nil {
		t.Fatal(err)
	}
	return dataset
}

func TestDatasetSelection(t *testing.T) {
	source := &Catalog{Data: []Product{
		{Name: OCPProductName},
		{Name: "Z", Package: "zeta"},
		{Name: "Shared", Package: " beta, alpha, beta, "},
		{Name: "Other alpha", Package: "alpha"},
	}}
	tests := []struct {
		name     string
		packages []string
		want     []string
		indices  []int
		missing  []string
	}{
		{"all", nil, []string{" beta, alpha, beta, ", "alpha", "zeta"}, []int{2, 3, 1}, nil},
		{"empty", []string{}, nil, nil, nil},
		{"selected", []string{"zeta", "alpha"}, []string{"alpha", "alpha", "zeta"}, []int{2, 3, 1}, nil},
		{"missing", []string{"missing"}, nil, nil, []string{"missing"}},
		{"partial", []string{"beta", "missing", "absent"}, []string{"beta"}, []int{2}, []string{"missing", "absent"}},
		{"repeated request", []string{"alpha", "alpha"}, []string{"alpha", "alpha"}, []int{2, 3}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := mustDataset(t, source, DatasetOptions{Packages: tt.packages, Validators: []string{"none"}})
			var names []string
			for _, product := range d.Catalog().Data {
				names = append(names, product.Package)
			}
			if !slices.Equal(names, tt.want) {
				t.Errorf("selected names = %v, want %v", names, tt.want)
			}
			if !slices.Equal(d.MissingPackages(), tt.missing) {
				t.Errorf("missing = %v, want %v", d.MissingPackages(), tt.missing)
			}
			var indices []int
			for _, product := range d.Validate().Products {
				indices = append(indices, product.SourceIndex)
			}
			if !slices.Equal(indices, tt.indices) {
				t.Errorf("source indices = %v, want %v", indices, tt.indices)
			}
			if !reflect.DeepEqual(d.Source(), source) {
				t.Error("selection changed the source")
			}
		})
	}
}

func TestDatasetSnapshots(t *testing.T) {
	source := &Catalog{Data: []Product{{
		Package: "operator",
		Versions: []Version{{Name: "1.0", Phases: []Phase{{
			Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z",
		}}}},
	}}}
	want, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	options := DatasetOptions{Packages: []string{"operator", "absent"}, Validators: []string{"REQ-VER-01"}}
	d := mustDataset(t, source, options)
	// All input and output slices may be changed without changing the dataset.
	options.Packages[0] = "changed"
	options.Validators[0] = "none"
	metadata := d.Validators()
	metadata[0].Label = "changed"
	missing := d.MissingPackages()
	missing[0] = "changed"
	for _, catalog := range []*Catalog{source, d.Source(), d.Catalog()} {
		catalog.Data[0].Package = "changed"
		catalog.Data[0].Versions[0].Name = "changed"
		catalog.Data[0].Versions[0].Phases[0].StartDate = "changed"
	}
	for _, catalog := range []*Catalog{d.Source(), d.Catalog()} {
		got, err := json.Marshal(catalog)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("dataset shares mutable data: got %s, want %s", got, want)
		}
	}
	if got := d.Validators(); len(got) != 1 || got[0].Label != "REQ-VER-01" {
		t.Errorf("validator metadata changed: %v", got)
	}
	if !slices.Equal(d.MissingPackages(), []string{"absent"}) {
		t.Errorf("missing packages changed: %v", d.MissingPackages())
	}
	if report := d.Validate(); len(report.Products) != 1 || len(report.Products[0].Failures) != 0 {
		t.Errorf("input mutation affected validation: %+v", report)
	}
}

func TestDatasetFindingsAndFiltering(t *testing.T) {
	source := &Catalog{Data: []Product{
		{Name: OCPProductName},
		{Package: "zeta", Versions: []Version{{Name: "1.0"}}},
		{Package: "alpha,beta", Versions: []Version{{Name: "bad"}}},
		{Package: "beta", Versions: []Version{{Name: "2.0"}}},
	}}
	d := mustDataset(t, source, DatasetOptions{Validators: []string{"REQ-VER-01", "REQ-VAL-01"}})
	if err := d.FilterInvalid(); err == nil {
		t.Fatal("filtering before validation succeeded")
	}
	catalogFailure := ValidationFailure{
		Validator: ValidatorInfo{Label: "REQ-VAL-01", Group: "catalog", Scope: CatalogScope},
		Packages:  []string{"beta"},
		Reasons:   []string{`REQ-VAL-01: package "beta" appears in 2 products`},
	}
	want := ValidationReport{Products: []ProductValidation{
		{SourceIndex: 2, Packages: []string{"alpha", "beta"}, Failures: []ValidationFailure{
			catalogFailure,
			{
				Validator: ValidatorInfo{Label: "REQ-VER-01", Group: "syntax", Scope: ProductScope},
				Packages:  []string{"alpha", "beta"},
				Reasons:   []string{`REQ-VER-01: version name "bad" is not MAJOR.MINOR`},
			},
		}},
		{SourceIndex: 3, Packages: []string{"beta"}, Failures: []ValidationFailure{catalogFailure}},
		{SourceIndex: 1, Packages: []string{"zeta"}},
	}}
	report := d.Validate()
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	if d.Catalog().Len() != 3 || !reflect.DeepEqual(d.Source(), source) {
		t.Fatal("validation filtered working data or changed the source")
	}
	// Corrupt every mutable level of a returned report before filtering.
	report.Products[0].SourceIndex = 1
	report.Products[0].Packages[0] = "changed"
	report.Products[0].Failures[0].Validator.Label = "changed"
	report.Products[0].Failures[0].Packages[0] = "changed"
	report.Products[0].Failures[0].Reasons[0] = "changed"
	report.Products[1].Failures = nil
	for range 2 {
		if err := d.FilterInvalid(); err != nil {
			t.Fatal(err)
		}
		if got := d.Catalog().Data; len(got) != 1 || got[0].Package != "zeta" {
			t.Fatalf("filtered products = %+v, want only zeta", got)
		}
		if got := d.Validate(); !reflect.DeepEqual(got, want) {
			t.Errorf("saved report changed after filtering: %+v", got)
		}
	}
	if !reflect.DeepEqual(d.Source(), source) || len(d.MissingPackages()) != 0 {
		t.Error("filtered products were removed from the source or treated as missing")
	}
}

func TestDatasetSelectedCatalogRules(t *testing.T) {
	source := &Catalog{Data: []Product{{Package: "alpha,beta"}, {Package: "beta"}}}
	d := mustDataset(t, source, DatasetOptions{Packages: []string{"alpha"}, Validators: []string{"catalog"}})
	report := d.Validate()
	if len(report.Products) != 1 || len(report.Products[0].Failures) != 0 {
		t.Fatalf("unselected duplicate affected selected alpha: %+v", report)
	}
	if err := d.FilterInvalid(); err != nil {
		t.Fatal(err)
	}
	if got := d.Catalog().Data; len(got) != 1 || got[0].Package != "alpha" {
		t.Errorf("selected product = %+v, want alpha", got)
	}
}

func TestDatasetFullSourceValidatorContext(t *testing.T) {
	phases := []Phase{
		{Name: PhaseFullSupport, StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
		{Name: PhaseMaintenance, StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
	}
	source := &Catalog{Data: []Product{
		{Name: OCPProductName, Versions: []Version{{Name: "4.17", Phases: phases}}},
		{Package: "operator", Versions: []Version{{Name: "1.0", Tier: TierAligned, OpenShiftCompatibility: "4.17", Phases: phases}}},
	}}
	d := mustDataset(t, source, DatasetOptions{Packages: []string{"operator"}, Validators: []string{"REQ-TIER-PA-01"}})
	// The bound rule must use the dataset snapshot, even if a caller changes OCP.
	source.Data[0].Versions[0].Name = "changed"
	copyOfSource := d.Source()
	copyOfSource.Data[0].Versions[0].Name = "also changed"
	report := d.Validate()
	if len(report.Products) != 1 || len(report.Products[0].Failures) != 0 {
		t.Fatalf("OCP context lost after selection: %+v", report)
	}
	withoutOCP := mustDataset(t, &Catalog{Data: source.Data[1:]}, DatasetOptions{Validators: []string{"REQ-TIER-PA-01"}})
	if got := withoutOCP.Validate(); len(got.Products[0].Failures) == 0 {
		t.Error("missing OCP context should require the additional EUS phases")
	}
}

func TestDatasetValidatorSelection(t *testing.T) {
	tests := []struct {
		name       string
		validators []string
		groups     []string
	}{
		{"default", nil, []string{"syntax", "semantic", "catalog"}},
		{"empty", []string{}, []string{"syntax", "semantic", "catalog"}},
		{"all", []string{"all"}, []string{"syntax", "semantic", "catalog"}},
		{"none", []string{"none"}, nil},
		{"syntax", []string{"syntax"}, []string{"syntax"}},
		{"semantic", []string{"semantic"}, []string{"semantic"}},
		{"catalog", []string{"catalog"}, []string{"catalog"}},
		{"daily", []string{"syntax", "catalog"}, []string{"syntax", "catalog"}},
		{"individual", []string{"REQ-VER-01"}, []string{"syntax"}},
		{"overlapping", []string{"syntax", "REQ-VER-01", "syntax"}, []string{"syntax"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := mustDataset(t, &Catalog{}, DatasetOptions{Validators: tt.validators})
			seen := make(map[string]bool)
			groups := make(map[string]bool)
			for _, rule := range d.Validators() {
				if seen[rule.Label] {
					t.Errorf("repeated rule: %s", rule.Label)
				}
				seen[rule.Label] = true
				groups[rule.Group] = true
				wantScope := ProductScope
				if rule.Group == "catalog" {
					wantScope = CatalogScope
				}
				if rule.Scope != wantScope {
					t.Errorf("rule %s scope = %s, want %s", rule.Label, rule.Scope, wantScope)
				}
			}
			if len(groups) != len(tt.groups) {
				t.Errorf("groups = %v, want %v", groups, tt.groups)
			}
			for _, group := range tt.groups {
				if !groups[group] {
					t.Errorf("missing group %q", group)
				}
			}
			if tt.name == "individual" && len(seen) != 1 {
				t.Errorf("individual label selected %d rules", len(seen))
			}
			if len(d.Validate().Products) != 0 {
				t.Error("empty source produced product findings")
			}
			if err := d.FilterInvalid(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, validators := range [][]string{{"unknown"}, {"none", "syntax"}, {"catalog", "none"}} {
		if d, err := NewDataset(&Catalog{}, DatasetOptions{Validators: validators}); err == nil || d != nil {
			t.Errorf("invalid validators %v returned dataset %v, error %v", validators, d, err)
		}
	}
	if d, err := NewDataset(nil, DatasetOptions{}); err == nil || d != nil {
		t.Errorf("nil source returned dataset %v, error %v", d, err)
	}
}

func TestDatasetValidationDisabled(t *testing.T) {
	source := &Catalog{Data: []Product{{Package: "duplicate"}, {Package: "duplicate"}}}
	d := mustDataset(t, source, DatasetOptions{Validators: []string{"none"}})
	for _, product := range d.Validate().Products {
		if len(product.Failures) != 0 {
			t.Errorf("disabled validation produced findings: %+v", product)
		}
	}
	if err := d.FilterInvalid(); err != nil {
		t.Fatal(err)
	}
	if d.Catalog().Len() != 2 {
		t.Fatal("disabled validation removed products")
	}
}

func TestDatasetRegistryMetadataAndCaching(t *testing.T) {
	previous := validatorRegistry
	t.Cleanup(func() { validatorRegistry = previous })
	calls := 0
	validatorRegistry = []validatorEntry{{Label: "TEST-01", Group: "syntax", Validators: []Validator{
		func(Product) []string { calls++; return []string{"reason without a rule prefix"} },
		func(Product) []string { calls++; return []string{"another reason"} },
	}}}
	d := mustDataset(t, &Catalog{Data: []Product{{Package: "operator"}}}, DatasetOptions{Validators: []string{"TEST-01"}})
	want := ValidationFailure{
		Validator: ValidatorInfo{Label: "TEST-01", Group: "syntax", Scope: ProductScope},
		Packages:  []string{"operator"},
		Reasons:   []string{"reason without a rule prefix", "another reason"},
	}
	for range 2 {
		got := d.Validate().Products[0].Failures
		if !reflect.DeepEqual(got, []ValidationFailure{want}) {
			t.Errorf("failures = %+v, want %+v", got, want)
		}
	}
	if calls != 2 {
		t.Errorf("callbacks called %d times, want once each", calls)
	}
	if err := d.FilterInvalid(); err != nil {
		t.Fatal(err)
	}
	if d.Catalog().Len() != 0 {
		t.Error("product-only rejection was retained")
	}
}

func TestDatasetValidatorInitializationError(t *testing.T) {
	previous := validatorRegistry
	t.Cleanup(func() { validatorRegistry = previous })
	wantErr := errors.New("initialization failed")
	validatorRegistry = []validatorEntry{{Label: "TEST-INIT", Group: "syntax", Init: func(*Catalog) ([]Validator, error) {
		return nil, wantErr
	}}}
	d, err := NewDataset(&Catalog{}, DatasetOptions{Validators: []string{"TEST-INIT"}})
	if d != nil || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "TEST-INIT") {
		t.Errorf("initialization failure returned dataset %v, error %v", d, err)
	}
}

func TestDatasetMatchesLegacyFiltering(t *testing.T) {
	source, err := Load("../fbc/testdata/plcc.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, validators := range [][]string{{"all"}, {"none"}, {"syntax", "catalog"}} {
		t.Run(strings.Join(validators, ","), func(t *testing.T) {
			d := mustDataset(t, source, DatasetOptions{Validators: validators})
			legacy := d.Source()
			productRules, catalogRules, err := legacy.LookupValidators(validators...)
			if err != nil {
				t.Fatal(err)
			}
			legacy.DropWithoutPackageName()
			legacy.SortByPackage()
			if len(catalogRules) > 0 {
				legacy.Validate(true, catalogRules...)
			}
			var accepted []Product
			for _, product := range legacy.Data {
				if len(ValidateProduct(product, productRules...)) == 0 {
					accepted = append(accepted, product)
				}
			}
			d.Validate()
			if err := d.FilterInvalid(); err != nil {
				t.Fatal(err)
			}
			if got := d.Catalog().Data; !slices.EqualFunc(got, accepted, func(a, b Product) bool { return reflect.DeepEqual(a, b) }) {
				t.Error("dataset and legacy pipeline accepted different products")
			}
		})
	}
}
