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
	"fmt"
	"slices"
	"sort"
)

// DatasetOptions fixes the package and validator selections for one dataset.
type DatasetOptions struct {
	// Packages contains individual package names. Nil selects every product
	// with a package name; a non-nil empty slice selects no products.
	Packages []string
	// Validators contains rule labels or groups. Empty selects "all";
	// use []string{"none"} to disable PLCC validation.
	Validators []string
}

// Dataset owns a PLCC source snapshot and a working catalog for one fixed
// selection. Construct it with NewDataset, then Validate before FilterInvalid.
// Methods return independent copies. A Dataset is not safe for concurrent use.
type Dataset struct {
	source        Catalog
	live          Catalog
	sourceIndices []int
	validators    []resolvedValidator
	missing       []string
	report        *ValidationReport
}

// NewDataset snapshots source, resolves validators against the full source,
// and selects working products in stable package order. Selection narrows
// comma-separated package names without expanding products. Missing requested
// names are available through MissingPackages and do not cause an error.
func NewDataset(source *Catalog, options DatasetOptions) (*Dataset, error) {
	if source == nil {
		return nil, fmt.Errorf("PLCC source catalog is nil")
	}
	d := &Dataset{source: cloneCatalog(*source)}
	names := options.Validators
	if len(names) == 0 {
		names = []string{"all"}
	}
	var err error
	d.validators, err = resolveValidators(&d.source, names)
	if err != nil {
		return nil, err
	}
	selected, missing := selectProducts(d.source.Data, options.Packages)
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].product.Package < selected[j].product.Package
	})
	d.live.Data = make([]Product, len(selected))
	d.sourceIndices = make([]int, len(selected))
	for i, entry := range selected {
		d.live.Data[i] = entry.product
		d.sourceIndices[i] = entry.sourceIndex
	}
	d.live = cloneCatalog(d.live)
	d.missing = missing
	return d, nil
}

// Source returns an independent copy of the complete original catalog,
// including products without package names and products excluded by selection.
func (d *Dataset) Source() *Catalog {
	catalog := cloneCatalog(d.source)
	return &catalog
}

// Catalog returns an independent copy of the selected working catalog,
// excluding rejected products only after FilterInvalid has been called.
func (d *Dataset) Catalog() *Catalog {
	catalog := cloneCatalog(d.live)
	return &catalog
}

// Validators returns the resolved rules in registry order, with duplicates
// removed. Metadata describes rules, each of which may run several callbacks.
func (d *Dataset) Validators() []ValidatorInfo {
	result := make([]ValidatorInfo, len(d.validators))
	for i, rule := range d.validators {
		result[i] = rule.info
	}
	return result
}

// MissingPackages returns requested names absent from the source, in request
// order. Products rejected during validation are not considered missing.
func (d *Dataset) MissingPackages() []string {
	return slices.Clone(d.missing)
}

// Validate records every selected catalog and product rule failure without
// changing either catalog. It returns an independent copy of the report.
// Subsequent calls return the saved report, even after filtering.
func (d *Dataset) Validate() ValidationReport {
	if d.report != nil {
		return cloneValidationReport(*d.report)
	}
	report := ValidationReport{Products: make([]ProductValidation, len(d.live.Data))}
	for i, product := range d.live.Data {
		report.Products[i] = ProductValidation{
			SourceIndex: d.sourceIndices[i],
			Packages:    product.Packages(),
		}
	}
	// Catalog rules run once across the selection. Attach each rejection to
	// every affected product, retaining the particular package it targets.
	for _, rule := range d.validators {
		if rule.info.Scope != CatalogScope {
			continue
		}
		rejections := validateCatalog(d.live.Data, rule.catalog...)
		for i := range report.Products {
			product := &report.Products[i]
			for _, name := range product.Packages {
				reasons, rejected := rejections[name]
				if rejected {
					product.Failures = append(product.Failures, ValidationFailure{
						Validator: rule.info,
						Packages:  []string{name},
						Reasons:   slices.Clone(reasons),
					})
				}
			}
		}
	}
	// Product rules also run for products with catalog failures, so filtering
	// policy cannot hide additional data quality problems.
	for i, product := range d.live.Data {
		for _, rule := range d.validators {
			if rule.info.Scope != ProductScope {
				continue
			}
			reasons := validateProduct(product, rule.product...)
			if len(reasons) > 0 {
				report.Products[i].Failures = append(report.Products[i].Failures, ValidationFailure{
					Validator: rule.info,
					Packages:  slices.Clone(report.Products[i].Packages),
					Reasons:   reasons,
				})
			}
		}
	}
	d.report = &report
	return cloneValidationReport(report)
}

// FilterInvalid removes working products with any recorded failure. Validate
// must be called first. Filtering is idempotent and preserves the saved report.
// Omit this call to retain invalid products for permissive processing.
func (d *Dataset) FilterInvalid() error {
	if d.report == nil {
		return fmt.Errorf("validate before filtering invalid PLCC products")
	}
	rejected := make(map[int]bool)
	for _, product := range d.report.Products {
		if len(product.Failures) > 0 {
			rejected[product.SourceIndex] = true
		}
	}
	products := make([]Product, 0, len(d.live.Data))
	indices := make([]int, 0, len(d.sourceIndices))
	for i, product := range d.live.Data {
		if !rejected[d.sourceIndices[i]] {
			products = append(products, product)
			indices = append(indices, d.sourceIndices[i])
		}
	}
	d.live.Data = products
	d.sourceIndices = indices
	return nil
}
