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
	"regexp"
	"time"
)

// REQ-VER-01: Version numbers must be validated against strict semver (<major>.<minor>).
var MajorMinorRegex = regexp.MustCompile(`^\d+\.\d+$`)

// Filter is a pipeline callback that can mutate a Package, validate it, or both.
// A non-empty return means the package should be rejected.
type Filter func(*Package) []string

// DefaultFilters returns the standard processing pipeline for FBC package filtering.
func DefaultFilters() []Filter {
	return []Filter{
		FilterPointInTimePhases,
		FilterIncompletePhases,
		ValidateHasVersions,
		ValidateVersionNames,
		ValidatePhases,
		ValidateOCPCompatibility,
	}
}

// Filter runs filters in order, stopping at the first one that returns reasons.
func (p *Package) Filter(filters ...Filter) []string {
	for _, f := range filters {
		if reasons := f(p); len(reasons) > 0 {
			return reasons
		}
	}
	return nil
}

// FilterPointInTimePhases checks that phases with only one date set (point-in-time)
// are properly positioned: a phase with no start must appear before the first complete
// phase and its end must match that phase's start; a phase with no end must appear after
// the last complete phase and its start must match that phase's end.
func FilterPointInTimePhases(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		type indexedPhase struct {
			index int
			phase Phase
		}
		var complete, pointInTime []indexedPhase
		for i, ph := range v.Phases {
			startEmpty := ph.StartDate == ""
			endEmpty := ph.EndDate == ""
			switch {
			case startEmpty && endEmpty:
				// not applicable, ignore
			case !startEmpty && !endEmpty:
				complete = append(complete, indexedPhase{i, ph})
			default:
				pointInTime = append(pointInTime, indexedPhase{i, ph})
			}
		}

		if len(complete) == 0 || len(pointInTime) == 0 {
			continue
		}

		first := complete[0]
		last := complete[len(complete)-1]

		for _, pt := range pointInTime {
			ph := pt.phase
			if ph.StartDate == "" {
				if pt.index < first.index && ph.EndDate == first.phase.StartDate {
					continue
				}
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-03: version %q phase %q: point-in-time (start unset, end %s) not aligned with first phase start (%s)",
					v.Name, ph.Name, ph.EndDate, first.phase.StartDate))
			} else {
				if pt.index > last.index && ph.StartDate == last.phase.EndDate {
					continue
				}
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-03: version %q phase %q: point-in-time (start %s, end unset) not aligned with last phase end (%s)",
					v.Name, ph.Name, ph.StartDate, last.phase.EndDate))
			}
		}
	}
	return reasons
}

// FilterIncompletePhases removes phases where either date is empty from all versions.
// REQ-DATE-03: API dates must cleanly parse as valid date values.
func FilterIncompletePhases(p *Package) []string {
	for i := range p.Versions {
		filtered := p.Versions[i].Phases[:0]
		for _, ph := range p.Versions[i].Phases {
			if ph.StartDate != "" && ph.EndDate != "" {
				filtered = append(filtered, ph)
			}
		}
		p.Versions[i].Phases = filtered
	}
	return nil
}

// ValidateHasVersions checks that the package has at least one version.
func ValidateHasVersions(p *Package) []string {
	if len(p.Versions) == 0 {
		return []string{"REQ-DATE-03: package has no versions"}
	}
	return nil
}

// ValidateVersionNames checks that all version names match MAJOR.MINOR format.
// REQ-VER-01: Version numbers must be validated against strict semver (<major>.<minor>).
func ValidateVersionNames(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		if !MajorMinorRegex.MatchString(v.Name) {
			reasons = append(reasons, fmt.Sprintf("REQ-VER-01: version name %q is not MAJOR.MINOR", v.Name))
		}
	}
	return reasons
}

// ValidatePhases checks that all phases have non-empty dates, end > start, and
// consecutive phases are continuous (each starts one day after the previous ends).
// REQ-DATE-01: Dates must not accept free-form text; must be machine-parseable.
// REQ-DATE-02: All dates must resolve to static, absolute values.
func ValidatePhases(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		if len(v.Phases) == 0 {
			reasons = append(reasons, fmt.Sprintf("REQ-DATE-01: version %q: no phases", v.Name))
			continue
		}

		var validPhases []Phase
		for _, ph := range v.Phases {
			if ph.StartDate == "" {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-01: version %q phase %q: missing start date", v.Name, ph.Name))
				continue
			}
			if ph.EndDate == "" {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-01: version %q phase %q: missing end date", v.Name, ph.Name))
				continue
			}
			start, err := time.Parse("2006-01-02", ph.StartDate)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-02: version %q phase %q: invalid start date format %q", v.Name, ph.Name, ph.StartDate))
				continue
			}
			end, err := time.Parse("2006-01-02", ph.EndDate)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-02: version %q phase %q: invalid end date format %q", v.Name, ph.Name, ph.EndDate))
				continue
			}
			if !end.After(start) {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-01: version %q phase %q: end (%s) is not after start (%s)", v.Name, ph.Name, ph.EndDate, ph.StartDate))
				continue
			}
			validPhases = append(validPhases, ph)
		}

		for i := 1; i < len(validPhases); i++ {
			prevEnd, _ := time.Parse("2006-01-02", validPhases[i-1].EndDate)
			currStart, _ := time.Parse("2006-01-02", validPhases[i].StartDate)
			expectedStart := prevEnd.AddDate(0, 0, 1)
			if !currStart.Equal(expectedStart) {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-01: version %q phase %q: start (%s) must be one day after previous phase %q end (%s)",
					v.Name, validPhases[i].Name, validPhases[i].StartDate, validPhases[i-1].Name, validPhases[i-1].EndDate))
			}
		}
	}
	return reasons
}

// ValidateOCPCompatibility checks that all OCP platform versions match MAJOR.MINOR format.
// REQ-FIELD-02: Aligned OCP version via existing openshift_compatibility field.
func ValidateOCPCompatibility(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, platform := range v.PlatformCompatibility {
			if platform.Name != "openshift" {
				continue
			}
			for _, ver := range platform.Versions {
				if !MajorMinorRegex.MatchString(ver) {
					reasons = append(reasons, fmt.Sprintf("REQ-FIELD-02: version %q: OCP compatibility %q is not MAJOR.MINOR", v.Name, ver))
				}
			}
		}
	}
	return reasons
}
