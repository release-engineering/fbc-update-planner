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
	"reflect"
	"testing"
	"time"
)

func TestPackages(t *testing.T) {
	tests := []struct {
		name string
		pkg  string
		want []string
	}{
		{"single name", "alpha", []string{"alpha"}},
		{"simple pair", "alpha,beta", []string{"alpha", "beta"}},
		{"trailing comma", "alpha,", []string{"alpha"}},
		{"double comma", "alpha,,beta", []string{"alpha", "beta"}},
		{"spaces trimmed", " alpha , beta ", []string{"alpha", "beta"}},
		{"intra-product dedup", "alpha,alpha,beta", []string{"alpha", "beta"}},
		{"only commas", ",", nil},
		{"empty string", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Product{Package: tt.pkg}
			got := p.Packages()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Packages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindProductByName(t *testing.T) {
	catalog := &Catalog{Data: []Product{
		{Name: "Product A", Package: "pkg-a"},
		{Name: "Product B", Package: "pkg-b"},
	}}

	if p := catalog.FindProductByName("Product A"); p == nil || p.Package != "pkg-a" {
		t.Errorf("expected pkg-a, got %v", p)
	}
	if p := catalog.FindProductByName("No Such Product"); p != nil {
		t.Errorf("expected nil, got %v", p)
	}

	empty := &Catalog{}
	if p := empty.FindProductByName("Product A"); p != nil {
		t.Errorf("expected nil on empty catalog, got %v", p)
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{"valid", "2025-11-11T00:00:00.000Z", time.Date(2025, 11, 11, 0, 0, 0, 0, time.UTC), false},
		{"N/A", "N/A", time.Time{}, true},
		{"empty", "", time.Time{}, true},
		{"malformed", "2025-13-01", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimestamp(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	got := FormatDate(time.Date(2025, 3, 5, 14, 30, 0, 0, time.UTC))
	if got != "2025-03-05" {
		t.Errorf("got %q, want %q", got, "2025-03-05")
	}
}

func TestExpandPackages(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "Single", Package: "pkg-a"},
		{Name: "Multi", Package: "beta-op,alpha-op"},
		{Name: "Empty", Package: ""},
	}}
	c.ExpandPackages()
	if len(c.Data) != 4 {
		t.Fatalf("got %d products, want 4", len(c.Data))
	}
	want := []string{"pkg-a", "beta-op", "alpha-op", ""}
	for i, w := range want {
		if c.Data[i].Package != w {
			t.Errorf("Data[%d].Package = %q, want %q", i, c.Data[i].Package, w)
		}
	}
	if c.Data[1].Name != "Multi" || c.Data[2].Name != "Multi" {
		t.Errorf("expanded products should preserve Name: got %q, %q", c.Data[1].Name, c.Data[2].Name)
	}
}

func TestDumpLoadRoundTrip(t *testing.T) {
	original := &Catalog{Data: []Product{
		{
			Name:    "Product A",
			Package: "pkg-a",
			Versions: []Version{{
				Name: "1.0",
				Phases: []Phase{{
					Name:      "Full support",
					StartDate: "2025-01-01T00:00:00.000Z",
					EndDate:   "2025-12-31T00:00:00.000Z",
				}},
				OpenShiftCompatibility: "4.12, 4.13",
			}},
		},
		{
			Name:    "Product B",
			Package: "pkg-b",
		},
	}}

	path := t.TempDir() + "/dump.json"
	if err := original.Dump(path); err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", loaded, original)
	}
}

func TestPackagesNotFoundErrorMessage(t *testing.T) {
	err := &PackagesNotFoundError{Names: []string{"foo", "bar"}}
	want := "packages not found in PLCC data: foo, bar"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestSortByPackage(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Package: "zebra"},
		{Package: "alpha"},
		{Package: "mid"},
	}}
	c.SortByPackage()
	want := []string{"alpha", "mid", "zebra"}
	for i, p := range c.Data {
		if p.Package != want[i] {
			t.Errorf("index %d: got %q, want %q", i, p.Package, want[i])
		}
	}
}
