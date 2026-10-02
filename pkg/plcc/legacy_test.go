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
	"errors"
	"testing"
)

// These defaults intentionally differ from the new dataset API.
func TestLegacyEmptySelections(t *testing.T) {
	t.Run("nil package list selects none", func(t *testing.T) {
		catalog := &Catalog{Data: []Product{{Package: "operator"}}}
		if err := catalog.FilterByPackageNames(nil); err != nil {
			t.Fatal(err)
		}
		if catalog.Data == nil || len(catalog.Data) != 0 {
			t.Errorf("expected an empty, non-nil catalog, got %+v", catalog.Data)
		}
	})
	t.Run("empty validator lookup selects none", func(t *testing.T) {
		product, catalog, err := (&Catalog{}).LookupValidators()
		if err != nil || len(product) != 0 || len(catalog) != 0 {
			t.Errorf("empty lookup returned %d product rules, %d catalog rules, error %v", len(product), len(catalog), err)
		}
	})
	t.Run("catalog validation defaults to duplicate check", func(t *testing.T) {
		catalog := &Catalog{Data: []Product{{Package: "duplicate"}, {Package: "duplicate"}}}
		rejections := catalog.Validate(true)
		if len(rejections["duplicate"]) == 0 || catalog.Len() != 0 {
			t.Errorf("default catalog validation: rejections %v, catalog %+v", rejections, catalog)
		}
	})
}

func TestLegacySelectionPreservesOrder(t *testing.T) {
	for _, selectPackages := range []bool{false, true} {
		catalog := &Catalog{Data: []Product{{Package: "zeta"}, {}, {Package: "alpha,beta"}}}
		if selectPackages {
			if err := catalog.FilterByPackageNames([]string{"alpha", "zeta", "beta"}); err != nil {
				t.Fatal(err)
			}
		} else {
			catalog.DropWithoutPackageName()
		}
		if len(catalog.Data) != 2 || catalog.Data[0].Package != "zeta" || catalog.Data[1].Package != "alpha,beta" {
			t.Errorf("legacy selection changed source order: %+v", catalog.Data)
		}
	}
}

func TestDropWithoutPackageName(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "A", Package: "pkg-a"},
		{Name: "B", Package: ""},
		{Name: "C", Package: "pkg-c"},
	}}
	c.DropWithoutPackageName()
	if len(c.Data) != 2 {
		t.Fatalf("got %d products, want 2", len(c.Data))
	}
	if c.Data[0].Package != "pkg-a" || c.Data[1].Package != "pkg-c" {
		t.Errorf("unexpected packages: %q, %q", c.Data[0].Package, c.Data[1].Package)
	}
}

func TestFilterByPackageNames(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "A", Package: "pkg-a"},
		{Name: "B", Package: "pkg-b"},
		{Name: "C", Package: "pkg-c"},
		{Name: "D", Package: "pkg-d"},
	}}
	err := c.FilterByPackageNames([]string{"pkg-a", "pkg-c"})
	if len(c.Data) != 2 {
		t.Fatalf("got %d products, want 2", len(c.Data))
	}
	if c.Data[0].Package != "pkg-a" || c.Data[1].Package != "pkg-c" {
		t.Errorf("unexpected packages: %q, %q", c.Data[0].Package, c.Data[1].Package)
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestFilterByPackageNamesNoMatch(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "A", Package: "pkg-a"},
		{Name: "B", Package: "pkg-b"},
	}}
	err := c.FilterByPackageNames([]string{"nonexistent"})
	if len(c.Data) != 0 {
		t.Fatalf("got %d products, want 0", len(c.Data))
	}
	var pkgErr *PackagesNotFoundError
	if !errors.As(err, &pkgErr) {
		t.Fatalf("expected PackagesNotFoundError, got %v", err)
	}
	if len(pkgErr.Names) != 1 || pkgErr.Names[0] != "nonexistent" {
		t.Errorf("expected [nonexistent], got %v", pkgErr.Names)
	}
}

func TestFilterByPackageNamesPartialMatch(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "A", Package: "pkg-a"},
		{Name: "B", Package: "pkg-b"},
	}}
	err := c.FilterByPackageNames([]string{"pkg-a", "missing-1", "missing-2"})
	if len(c.Data) != 1 || c.Data[0].Package != "pkg-a" {
		t.Fatalf("got %d products, want 1 (pkg-a)", len(c.Data))
	}
	var pkgErr *PackagesNotFoundError
	if !errors.As(err, &pkgErr) {
		t.Fatalf("expected PackagesNotFoundError, got %v", err)
	}
	if len(pkgErr.Names) != 2 || pkgErr.Names[0] != "missing-1" || pkgErr.Names[1] != "missing-2" {
		t.Errorf("expected [missing-1 missing-2], got %v", pkgErr.Names)
	}
}

func TestFilterByPackageNamesNarrowsCommaSeparated(t *testing.T) {
	c := &Catalog{Data: []Product{
		{Name: "Multi", Package: "alpha,beta"},
		{Name: "Single", Package: "gamma"},
	}}
	err := c.FilterByPackageNames([]string{"alpha", "gamma"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Data) != 2 {
		t.Fatalf("got %d products, want 2", len(c.Data))
	}
	if c.Data[0].Package != "alpha" {
		t.Errorf("expected Package narrowed to %q, got %q", "alpha", c.Data[0].Package)
	}
	if c.Data[1].Package != "gamma" {
		t.Errorf("expected Package %q, got %q", "gamma", c.Data[1].Package)
	}
}
