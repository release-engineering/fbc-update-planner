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

package assessment

import (
	"errors"
	"sort"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

type ValidationOptions struct {
	Packages          []string
	SelectPackages    bool
	Validators        []plcc.Validator
	CatalogValidators []plcc.CatalogValidator
	Strict            bool
	AllowMissing      bool
}

type ValidationResult struct {
	Catalog           *plcc.Catalog
	MissingPackages   []string
	Validation        []report.ValidationResult
	CatalogRejections plcc.CatalogRejections
	ProductRejections map[string][]string
	SelectedCount     int
	CatalogFiltered   int
	ProductFiltered   int
}

// Validate selects and checks products without mutating raw. Catalog checks
// run before product checks, matching the conversion pipeline's rejection
// order. ProductRejections is keyed by each expanded package name.
func Validate(raw *plcc.Catalog, opts ValidationOptions) (ValidationResult, error) {
	result := ValidationResult{
		Catalog:           &plcc.Catalog{Data: append([]plcc.Product(nil), raw.Data...)},
		CatalogRejections: make(plcc.CatalogRejections),
		ProductRejections: make(map[string][]string),
	}
	catalog := result.Catalog
	if opts.SelectPackages {
		if err := catalog.FilterByPackageNames(opts.Packages); err != nil {
			if !opts.AllowMissing {
				return ValidationResult{}, err
			}
			var missing *plcc.PackagesNotFoundError
			if !errors.As(err, &missing) {
				return ValidationResult{}, err
			}
			result.MissingPackages = missing.Names
		}
	} else {
		catalog.DropWithoutPackageName()
	}
	result.SelectedCount = catalog.Len()
	catalog.SortByPackage()

	if len(opts.CatalogValidators) > 0 {
		result.CatalogRejections = catalog.Validate(opts.Strict, opts.CatalogValidators...)
		result.CatalogFiltered = result.SelectedCount - catalog.Len()
		keys := make([]string, 0, len(result.CatalogRejections))
		for pkg := range result.CatalogRejections {
			keys = append(keys, pkg)
		}
		sort.Strings(keys)
		for _, pkg := range keys {
			result.Validation = append(result.Validation, report.ValidationResult{
				PackageName: pkg,
				Valid:       !opts.Strict,
				Reasons:     result.CatalogRejections[pkg],
			})
		}
	}

	beforeProduct := catalog.Len()
	filtered := make([]plcc.Product, 0, beforeProduct)
	for _, product := range catalog.Data {
		reasons := plcc.ValidateProduct(product, opts.Validators...)
		if len(reasons) == 0 {
			filtered = append(filtered, product)
			continue
		}
		for _, pkg := range product.Packages() {
			result.ProductRejections[pkg] = append(result.ProductRejections[pkg], reasons...)
		}
		result.Validation = append(result.Validation, report.ValidationResult{
			PackageName: product.Package,
			Valid:       !opts.Strict,
			Reasons:     reasons,
		})
		if !opts.Strict {
			filtered = append(filtered, product)
		}
	}
	if opts.Strict {
		result.ProductFiltered = beforeProduct - len(filtered)
		catalog.Data = filtered
	}
	return result, nil
}
