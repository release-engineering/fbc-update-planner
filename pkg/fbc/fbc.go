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

package fbc

import (
	"fmt"
	"slices"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

// Schema is the FBC schema identifier for operator lifecycle data.
const Schema = "io.openshift.operators.lifecycles.v1alpha1"

// Package represents an FBC lifecycle entry for a single operator package.
type Package struct {
	Schema   string    `json:"schema"`
	Name     string    `json:"package"`
	Versions []Version `json:"versions"`
}

// Version represents an operator version with its lifecycle phases and platform compatibility.
type Version struct {
	Name                  MajorMinor `json:"name"`
	Phases                []Phase    `json:"phases"`
	PlatformCompatibility []Platform `json:"platformCompatibility,omitempty"`
}

// Phase represents a lifecycle phase with start and end dates.
type Phase struct {
	Name      string `json:"name"`
	StartDate *Date  `json:"startDate,omitempty"`
	EndDate   *Date  `json:"endDate,omitempty"`
}

// Platform represents platform compatibility information.
type Platform struct {
	Name     string       `json:"name"`
	Versions []MajorMinor `json:"versions"`
}

// TranslateProduct converts a single PLCC product to an FBC package, running
// it through the provided filter pipeline. Returns the package on success, or
// nil and a ValidationResult on failure.
func TranslateProduct(product plcc.Product, filters ...Filter) (*Package, *report.ValidationResult) {
	pkg, errs := newPackage(product)
	if len(errs) > 0 {
		reasons := make([]string, len(errs))
		for i, e := range errs {
			reasons[i] = e.Error()
		}
		return nil, &report.ValidationResult{
			PackageName: product.Package,
			Valid:       false,
			Reasons:     reasons,
		}
	}
	reasons := pkg.Filter(filters...)
	if len(reasons) > 0 {
		return nil, &report.ValidationResult{
			PackageName: product.Package,
			Valid:       false,
			Reasons:     reasons,
		}
	}
	return pkg, nil
}

// Translate converts PLCC products to FBC packages, running each through the
// provided filter pipeline. Filters may mutate packages (e.g., drop incomplete
// phases) or reject them. Returns the valid packages and a list of rejections.
// Unlike the CLI's --split mode, Translate always processes all products and
// collects failures rather than aborting on the first one.
// Callers should run Catalog.Validate before calling Translate for cross-product
// checks such as duplicate package detection.
func Translate(products []plcc.Product, filters ...Filter) ([]*Package, []report.ValidationResult) {
	var failures []report.ValidationResult
	validPackages := make([]*Package, 0, len(products))
	for _, product := range products {
		for _, pkgName := range product.Packages() {
			single := product
			single.Package = pkgName
			pkg, failure := TranslateProduct(single, filters...)
			if failure != nil {
				failures = append(failures, *failure)
				continue
			}
			validPackages = append(validPackages, pkg)
		}
	}
	return validPackages, failures
}

func newPackage(product plcc.Product) (*Package, []error) {
	pkg := &Package{
		Schema: Schema,
		Name:   product.Package,
	}

	var errs []error
	for _, v := range product.Versions {
		fv, verErrs := translateVersion(v)
		if len(verErrs) > 0 {
			errs = append(errs, verErrs...)
			continue
		}
		pkg.Versions = append(pkg.Versions, *fv)
	}
	if len(errs) > 0 {
		return nil, errs
	}

	slices.SortFunc(pkg.Versions, func(a, b Version) int {
		return a.Name.Compare(b.Name)
	})

	return pkg, nil
}

func translateVersion(v plcc.Version) (*Version, []error) {
	dst := &Version{}
	var errs []error
	for _, entry := range converterRegistry {
		for _, conv := range entry.Converters {
			for _, e := range conv(v, dst) {
				errs = append(errs, fmt.Errorf("%s: version %q: %w", entry.Label, v.Name, e))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return dst, nil
}
