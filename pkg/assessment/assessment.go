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

// Package assessment joins raw PLCC products with the results of the shared
// validation and FBC translation pipeline.
package assessment

import (
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

type Package struct {
	Product            *plcc.Product
	CatalogReasons     []string
	ProductReasons     []string
	TranslationReasons []string
	FBC                *fbc.Package
}

type Result struct {
	Packages map[string]*Package
	FBC      []*fbc.Package
	Failures []report.ValidationResult
}

// Evaluate translates each validated package once when translate is true.
// The original PLCC product remains available even when validation rejects
// it, so catalog gaps need no joins over generated files.
func Evaluate(raw *plcc.Catalog, validated ValidationResult, translate bool) Result {
	result := Result{Packages: make(map[string]*Package)}
	for i := range raw.Data {
		product := &raw.Data[i]
		for _, name := range product.Packages() {
			if _, exists := result.Packages[name]; !exists {
				result.Packages[name] = &Package{Product: product}
			}
		}
	}
	for name, state := range result.Packages {
		state.CatalogReasons = validated.CatalogRejections[name]
		state.ProductReasons = validated.ProductRejections[name]
	}
	if !translate {
		return result
	}

	output := &plcc.Catalog{Data: append([]plcc.Product(nil), validated.Catalog.Data...)}
	output.ExpandPackages()
	output.SortByPackage()
	for _, product := range output.Data {
		pkg, failure := fbc.TranslateProduct(product, fbc.DefaultFilters()...)
		state := result.Packages[product.Package]
		if failure != nil {
			result.Failures = append(result.Failures, *failure)
			if state != nil {
				state.TranslationReasons = failure.Reasons
			}
			continue
		}
		result.FBC = append(result.FBC, pkg)
		if state != nil {
			state.FBC = pkg
		}
	}
	return result
}
