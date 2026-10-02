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

// DropWithoutPackageName removes products that have no package name, modifying the catalog in place.
//
// For new callers, prefer NewDataset with DatasetOptions.Packages set to nil.
func (c *Catalog) DropWithoutPackageName() {
	selected, _ := selectProducts(c.Data, nil)
	c.Data = make([]Product, len(selected))
	for i, entry := range selected {
		c.Data[i] = entry.product
	}
}

// FilterByPackageNames keeps only products where at least one expanded package name
// matches the provided list, modifying the catalog in place. It returns a
// PackagesNotFoundError if any names were not found. When a product has
// comma-separated names (e.g. "alpha-op,beta-op"), only the matched names are
// preserved in the Package field so downstream expansion emits only requested packages.
// The catalog is modified in place also in case of error.
//
// For new callers, prefer NewDataset and Dataset.MissingPackages.
func (c *Catalog) FilterByPackageNames(names []string) error {
	// The legacy method treats nil as an explicit empty selection.
	if names == nil {
		names = []string{}
	}
	selected, notFound := selectProducts(c.Data, names)
	c.Data = make([]Product, len(selected))
	for i, entry := range selected {
		c.Data[i] = entry.product
	}
	if len(notFound) > 0 {
		return &PackagesNotFoundError{Names: notFound}
	}
	return nil
}

// LookupValidators resolves a list of label or group names into per-product
// and catalog validators.
// Accepted group names: "all", "syntax", "semantic", "catalog".
// Accepted labels: any label in either registry (e.g. "REQ-DATE-03", "REQ-VAL-01").
// Call on the full catalog before filtering so that Init functions can look up
// cross-product context (e.g. OCP lifecycle data for platform-aligned checks).
// Returns an error if any name is unknown.
//
// For new callers, prefer NewDataset and Dataset.Validators.
func (c *Catalog) LookupValidators(names ...string) ([]Validator, []CatalogValidator, error) {
	rules, err := resolveValidators(c, names)
	if err != nil {
		return nil, nil, err
	}
	var prodResult []Validator
	var catResult []CatalogValidator
	for _, rule := range rules {
		prodResult = append(prodResult, rule.product...)
		catResult = append(catResult, rule.catalog...)
	}
	return prodResult, catResult, nil
}

// ValidateProduct runs all provided validators on a single product and returns
// the combined list of reasons. Returns nil if all validators pass.
//
// For new callers, prefer Dataset.Validate to retain rule metadata with findings.
func ValidateProduct(p Product, validators ...Validator) []string {
	return validateProduct(p, validators...)
}

// Validate runs catalog validators across the catalog's products and returns
// per-package reasons. If no validators are provided, uses
// DefaultCatalogValidators(). When strict is true, products that trigger catalog
// warnings (e.g. duplicated package names) are removed from c.Data.
//
// For new callers, prefer Dataset.Validate followed by Dataset.FilterInvalid.
func (c *Catalog) Validate(strict bool, validators ...CatalogValidator) CatalogRejections {
	if len(validators) == 0 {
		validators = DefaultCatalogValidators()
	}
	rejections := validateCatalog(c.Data, validators...)
	if strict && len(rejections) > 0 {
		var filtered []Product
		for _, p := range c.Data {
			rejected := false
			for _, pkg := range p.Packages() {
				if _, found := rejections[pkg]; found {
					rejected = true
					break
				}
			}
			if !rejected {
				filtered = append(filtered, p)
			}
		}
		c.Data = filtered
	}
	return rejections
}
