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
	"io"
	"sort"
	"strconv"
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
	Name                  string     `json:"name"`
	Phases                []Phase    `json:"phases"`
	PlatformCompatibility []Platform `json:"platformCompatibility,omitempty"`
}

// Phase represents a lifecycle phase with start and end dates.
type Phase struct {
	Name      string `json:"name"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// Platform represents platform compatibility information.
type Platform struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
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
func Translate(products []plcc.Product, filters ...Filter) ([]*Package, []report.ValidationResult) {
	var failures []report.ValidationResult
	validPackages := make([]*Package, 0, len(products))
	for _, product := range products {
		pkg := newPackage(product)
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

func newPackage(product plcc.Product) *Package {
	pkg := &Package{
		Schema: Schema,
		Name:   product.Package,
	}

	for _, v := range product.Versions {
		pkg.Versions = append(pkg.Versions, translateVersion(v))
	}

	sort.Slice(pkg.Versions, func(i, j int) bool {
		return compareMajorMinor(pkg.Versions[i].Name, pkg.Versions[j].Name) < 0
	})

	return pkg
}

func translateVersion(v plcc.Version) Version {
	fv := Version{Name: v.Name}

	for _, ph := range v.Phases {
		fv.Phases = append(fv.Phases, translatePhase(ph))
	}

	if v.OpenShiftCompatibility != "" && v.OpenShiftCompatibility != "N/A" {
		var ocpVersions []string
		for _, p := range strings.Split(v.OpenShiftCompatibility, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				ocpVersions = append(ocpVersions, trimmed)
			}
		}
		if len(ocpVersions) > 0 {
			fv.PlatformCompatibility = []Platform{{
				Name:     "openshift",
				Versions: ocpVersions,
			}}
		}
	}

	return fv
}

func translatePhase(ph plcc.Phase) Phase {
	start := ""
	if t, err := plcc.ParseTimestamp(ph.StartDate); err == nil {
		start = plcc.FormatDate(t)
	}
	end := ""
	if t, err := plcc.ParseTimestamp(ph.EndDate); err == nil {
		end = plcc.FormatDate(t)
	}
	return Phase{Name: ph.Name, StartDate: start, EndDate: end}
}

func compareMajorMinor(a, b string) int {
	aParts := strings.SplitN(a, ".", 2)
	bParts := strings.SplitN(b, ".", 2)
	if len(aParts) < 2 || len(bParts) < 2 {
		return strings.Compare(a, b)
	}
	aMajor, _ := strconv.Atoi(aParts[0])
	bMajor, _ := strconv.Atoi(bParts[0])
	if aMajor != bMajor {
		return aMajor - bMajor
	}
	aMinor, _ := strconv.Atoi(aParts[1])
	bMinor, _ := strconv.Atoi(bParts[1])
	return aMinor - bMinor
}
