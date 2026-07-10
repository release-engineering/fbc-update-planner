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
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

// Converter validates a field of plcc.Version and populates the corresponding
// field of fbc.Version. Returns errors for invalid data.
type Converter func(src plcc.Version, dst *Version) []error

type converterEntry struct {
	Label      string
	Group      string // "converter"
	Converters []Converter
}

var converterRegistry = []converterEntry{
	{"FBC-VER-01", "converter", []Converter{ConvertVersionName}},
	{"FBC-PHASE-01", "converter", []Converter{ConvertPhases}},
	{"FBC-OCP-01", "converter", []Converter{ConvertOCPCompatibility}},
}

// DefaultConverters returns the standard set of version converters.
func DefaultConverters() []Converter {
	var result []Converter
	for _, entry := range converterRegistry {
		result = append(result, entry.Converters...)
	}
	return result
}

// ConvertVersionName parses the version name as MAJOR.MINOR and sets dst.Name.
// FBC-VER-01
func ConvertVersionName(src plcc.Version, dst *Version) []error {
	name, err := ParseMajorMinor(src.Name)
	if err != nil {
		return []error{fmt.Errorf("FBC-VER-01: %w", err)}
	}
	dst.Name = name
	return nil
}

// ConvertPhases translates each PLCC phase into an FBC phase and sets dst.Phases.
// Phases with unparseable timestamps produce errors; empty/"N/A" timestamps
// become nil dates (later cleaned by FilterIncompletePhases).
// FBC-PHASE-01
func ConvertPhases(src plcc.Version, dst *Version) []error {
	var errs []error
	for _, ph := range src.Phases {
		fp, phaseErrs := translatePhase(ph)
		if len(phaseErrs) > 0 {
			for _, e := range phaseErrs {
				errs = append(errs, fmt.Errorf("FBC-PHASE-01: phase %q: %w", ph.Name, e))
			}
		} else {
			dst.Phases = append(dst.Phases, fp)
		}
	}
	return errs
}

// ConvertOCPCompatibility parses comma-separated OCP version strings as
// MAJOR.MINOR and sets dst.PlatformCompatibility. Empty and "N/A" values
// are ignored.
// FBC-OCP-01
func ConvertOCPCompatibility(src plcc.Version, dst *Version) []error {
	if src.OpenShiftCompatibility == "" || src.OpenShiftCompatibility == "N/A" {
		return nil
	}
	var errs []error
	var ocpVersions []MajorMinor
	for _, p := range strings.Split(src.OpenShiftCompatibility, ",") {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		mm, err := ParseMajorMinor(trimmed)
		if err != nil {
			errs = append(errs, fmt.Errorf("FBC-OCP-01: OCP compatibility: %w", err))
		} else {
			ocpVersions = append(ocpVersions, mm)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if len(ocpVersions) > 0 {
		dst.PlatformCompatibility = []Platform{{Name: "openshift", Versions: ocpVersions}}
	}
	return nil
}

func translatePhase(ph plcc.Phase) (Phase, []error) {
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
		return Phase{}, errs
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

