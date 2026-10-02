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
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

// PackagesNotFoundError is returned when requested package names are not found in the catalog.
type PackagesNotFoundError struct {
	Names []string
}

func (e *PackagesNotFoundError) Error() string {
	return fmt.Sprintf("packages not found in PLCC data: %s", strings.Join(e.Names, ", "))
}

// OCPProductName is the PLCC product name for OpenShift Container Platform.
const OCPProductName = "Red Hat OpenShift Container Platform"

// Catalog holds the product lifecycle data returned by the PLCC API.
type Catalog struct {
	Data []Product `json:"data"`
}

// Product represents a software product with its lifecycle versions.
type Product struct {
	Name           string    `json:"name"`
	Package        string    `json:"package"`
	Versions       []Version `json:"versions"`
	ReleaseCadence string    `json:"release_cadence"`
	IsOperator     bool      `json:"is_operator"`
}

// Packages returns the unique, trimmed, non-empty package names for this
// product. The package field may contain a comma-separated list (e.g.
// "odf-operator,mcg-operator"). Duplicate names within the list are collapsed.
func (p Product) Packages() []string {
	if p.Package == "" {
		return nil
	}
	parts := strings.Split(p.Package, ",")
	seen := make(map[string]struct{}, len(parts))
	var filtered []string
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// Version represents a product version with its lifecycle phases and platform compatibility.
type Version struct {
	Name                   string  `json:"name"`
	Phases                 []Phase `json:"phases"`
	OpenShiftCompatibility string  `json:"openshift_compatibility"`
	Tier                   string  `json:"tier"`
}

// Phase represents a lifecycle phase with start and end dates (ISO8601 timestamps).
type Phase struct {
	Name            string `json:"name"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	StartDateFormat string `json:"start_date_format"`
	EndDateFormat   string `json:"end_date_format"`
}

// ExpandPackages splits products with comma-separated package names into
// separate Product entries, one per package name. Products with a single
// package name are unchanged. Call this after validation to preserve original
// product shape in validation logs and --dump-plcc output.
func (c *Catalog) ExpandPackages() {
	var expanded []Product
	for _, p := range c.Data {
		names := p.Packages()
		if len(names) <= 1 {
			if len(names) == 1 {
				p.Package = names[0]
			}
			expanded = append(expanded, p)
			continue
		}
		for _, name := range names {
			clone := p
			clone.Package = name
			expanded = append(expanded, clone)
		}
	}
	c.Data = expanded
}

// FindProductByName returns a pointer to the first product matching the given
// name, or nil if no match is found.
func (c *Catalog) FindProductByName(name string) *Product {
	for i := range c.Data {
		if c.Data[i].Name == name {
			return &c.Data[i]
		}
	}
	return nil
}

// Len returns the number of products currently in the catalog.
func (c *Catalog) Len() int {
	return len(c.Data)
}

// SortByPackage sorts products by package name in ascending order.
func (c *Catalog) SortByPackage() {
	sort.Slice(c.Data, func(i, j int) bool {
		return c.Data[i].Package < c.Data[j].Package
	})
}

// Dump writes the catalog products to a JSON file.
func (c *Catalog) Dump(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// ParseTimestamp parses an ISO8601 timestamp as used by the PLCC API (e.g. "2007-06-01T00:00:00.000Z").
func ParseTimestamp(s string) (time.Time, error) {
	if s == "N/A" || s == "" {
		return time.Time{}, fmt.Errorf("timestamp is %q (unset)", s)
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid ISO8601 timestamp %q: %w", s, err)
	}
	return t, nil
}

// FormatDate formats a time value as "YYYY-MM-DD".
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

type selectedProduct struct {
	product     Product
	sourceIndex int
}

// selectProducts returns matching products in their original source order.
// It copies Product structs, but their Versions and Phases slices still
// share underlying arrays with source. NewDataset clones these slices
// before using the selected products as its working catalog.
func selectProducts(source []Product, names []string) ([]selectedProduct, []string) {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	found := make(map[string]bool, len(names))
	selected := make([]selectedProduct, 0, len(source))
	for i, product := range source {
		packages := product.Packages()
		if names == nil {
			if len(packages) > 0 {
				selected = append(selected, selectedProduct{product, i})
			}
			continue
		}
		var matches []string
		for _, name := range packages {
			if allowed[name] {
				matches = append(matches, name)
				found[name] = true
			}
		}
		if len(matches) > 0 {
			product.Package = strings.Join(matches, ",")
			selected = append(selected, selectedProduct{product, i})
		}
	}
	var missing []string
	for _, name := range names {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	return selected, missing
}

func cloneCatalog(c Catalog) Catalog {
	c.Data = slices.Clone(c.Data)
	for i := range c.Data {
		product := &c.Data[i]
		product.Versions = slices.Clone(product.Versions)
		for j := range product.Versions {
			product.Versions[j].Phases = slices.Clone(product.Versions[j].Phases)
		}
	}
	return c
}
