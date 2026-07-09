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
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

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

// GenerateFBC converts PLCC products to FBC data, writing valid packages to output
// using the provided PackageWriter and validation failures as JSON to logOutput.
// Returns the number of valid packages emitted.
func GenerateFBC(products []plcc.Product, output io.Writer, logOutput io.Writer, writer PackageWriter) (int, error) {
	if writer == nil {
		return 0, fmt.Errorf("PackageWriter must not be nil")
	}

	valid, failures := Translate(products, DefaultFilters()...)

	if err := report.LogResults(logOutput, failures...); err != nil {
		return 0, err
	}

	if err := writer.Write(output, valid...); err != nil {
		return 0, fmt.Errorf("failed to write packages: %w", err)
	}
	return len(valid), nil
}

// Translate converts PLCC products to FBC packages, running each through the
// provided filter pipeline. Filters may mutate packages (e.g., drop incomplete
// phases) or reject them. Returns the valid packages and a list of rejections.
// Callers should run Catalog.Validate before calling Translate for cross-product
// checks such as duplicate package detection.
func Translate(products []plcc.Product, filters ...Filter) ([]*Package, []report.ValidationResult) {
	var failures []report.ValidationResult
	validPackages := make([]*Package, 0, len(products))
	for _, product := range products {
		pkg, err := newPackage(product)
		if err != nil {
			var reasons []string
			for _, e := range unwrapJoined(err) {
				reasons = append(reasons, e.Error())
			}
			failures = append(failures, report.ValidationResult{
				PackageName: product.Package,
				Valid:       false,
				Reasons:     reasons,
			})
			continue
		}
		reasons := pkg.Filter(filters...)
		if len(reasons) > 0 {
			failures = append(failures, report.ValidationResult{
				PackageName: product.Package,
				Valid:       false,
				Reasons:     reasons,
			})
			continue
		}

		validPackages = append(validPackages, pkg)
	}

	return validPackages, failures
}

func newPackage(product plcc.Product) (*Package, error) {
	pkg := &Package{
		Schema: Schema,
		Name:   product.Package,
	}

	var errs []error
	for _, v := range product.Versions {
		fv, err := translateVersion(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("version %q: %w", v.Name, err))
			continue
		}
		pkg.Versions = append(pkg.Versions, *fv)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	slices.SortFunc(pkg.Versions, func(a, b Version) int {
		return a.Name.Compare(b.Name)
	})

	return pkg, nil
}

func translateVersion(v plcc.Version) (*Version, error) {
	var errs []error

	name, err := ParseMajorMinor(v.Name)
	if err != nil {
		errs = append(errs, err)
	}

	var phases []Phase
	for _, ph := range v.Phases {
		fp, err := translatePhase(ph)
		if err != nil {
			errs = append(errs, fmt.Errorf("phase %q: %w", ph.Name, err))
		} else {
			phases = append(phases, fp)
		}
	}

	var platforms []Platform
	if v.OpenShiftCompatibility != "" && v.OpenShiftCompatibility != "N/A" {
		var ocpVersions []MajorMinor
		for _, p := range strings.Split(v.OpenShiftCompatibility, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				continue
			}
			mm, err := ParseMajorMinor(trimmed)
			if err != nil {
				errs = append(errs, fmt.Errorf("OCP compatibility: %w", err))
			} else {
				ocpVersions = append(ocpVersions, mm)
			}
		}
		if len(ocpVersions) > 0 {
			platforms = []Platform{{Name: "openshift", Versions: ocpVersions}}
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	fv := &Version{
		Name:                  name,
		Phases:                phases,
		PlatformCompatibility: platforms,
	}
	return fv, nil
}

func translatePhase(ph plcc.Phase) (Phase, error) {
	var errs []error

	start, err := translateTimestamp(ph.StartDate)
	if err != nil {
		errs = append(errs, fmt.Errorf("start date: %w", err))
	}

	end, err := translateTimestamp(ph.EndDate)
	if err != nil {
		errs = append(errs, fmt.Errorf("end date: %w", err))
	}

	if len(errs) > 0 {
		return Phase{}, errors.Join(errs...)
	}
	return Phase{Name: ph.Name, StartDate: start, EndDate: end}, nil
}

func translateTimestamp(s string) (*Date, error) {
	if s == "" || s == "N/A" {
		return nil, nil
	}
	t, err := plcc.ParseTimestamp(s)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	d := NewDate(t.Year(), t.Month(), t.Day())
	return &d, nil
}

// unwrapJoined extracts individual errors from an errors.Join result.
func unwrapJoined(err error) []error {
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		return u.Unwrap()
	}
	return []error{err}
}
