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


func TestFilterPackages(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "A", Package: "pkg-a"},
		{Name: "B", Package: ""},
		{Name: "C", Package: "pkg-c"},
	}}
	c.FilterPackages()
	if len(c.Data) != 2 {
		t.Fatalf("got %d products, want 2", len(c.Data))
	}
	if c.Data[0].Package != "pkg-a" || c.Data[1].Package != "pkg-c" {
		t.Errorf("unexpected packages: %q, %q", c.Data[0].Package, c.Data[1].Package)
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

