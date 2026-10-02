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

import "slices"

// ValidationScope identifies whether a rule examines one product or the
// selected catalog. It is independent of the rule's selectable group.
type ValidationScope string

const (
	// ProductScope marks rules that examine one PLCC product.
	ProductScope ValidationScope = "product"
	// CatalogScope marks rules that examine the selected products together.
	CatalogScope ValidationScope = "catalog"
)

// ValidatorInfo identifies a registered validation rule.
type ValidatorInfo struct {
	Label string
	Group string
	Scope ValidationScope
}

// ValidationFailure is one rule's failure for a selected product.
type ValidationFailure struct {
	Validator ValidatorInfo
	// Packages identifies the rule's targets. A catalog failure targets the
	// rejected package; a product failure targets all its selected packages.
	// Either kind prevents filtering from retaining any part of the product.
	Packages []string
	// Reasons contains the original messages returned by the rule callbacks.
	Reasons []string
}

// ProductValidation keeps findings attached to a particular source product,
// even when multiple source products share package names.
type ProductValidation struct {
	// SourceIndex identifies the product in Dataset.Source().Data.
	SourceIndex int
	// Packages contains this product's selected, unique package names.
	Packages []string
	// Failures lists catalog failures first, then product failures, each in
	// registry order. Empty means the product passed the selected PLCC rules.
	Failures []ValidationFailure
}

// ValidationReport records every selected product, including valid products,
// in the working catalog's original stable package order. Filtering does not
// remove records. This reports PLCC validation, not FBC translatability.
type ValidationReport struct {
	Products []ProductValidation
}

func cloneValidationReport(report ValidationReport) ValidationReport {
	report.Products = slices.Clone(report.Products)
	for i := range report.Products {
		product := &report.Products[i]
		product.Packages = slices.Clone(product.Packages)
		product.Failures = slices.Clone(product.Failures)
		for j := range product.Failures {
			failure := &product.Failures[j]
			failure.Packages = slices.Clone(failure.Packages)
			failure.Reasons = slices.Clone(failure.Reasons)
		}
	}
	return report
}
