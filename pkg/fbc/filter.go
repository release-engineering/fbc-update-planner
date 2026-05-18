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
	"time"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

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
// are properly positioned: a phase with no begin must appear before the first complete
// phase and its end must match that phase's begin; a phase with no end must appear after
// the last complete phase and its begin must match that phase's end.
func FilterPointInTimePhases(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		type indexedPhase struct {
			index int
			phase Phase
		}
		var complete, pointInTime []indexedPhase
		for i, ph := range v.Phases {
			beginEmpty := ph.StartDate == ""
			endEmpty := ph.EndDate == ""
			switch {
			case beginEmpty && endEmpty:
				// not applicable, ignore
			case !beginEmpty && !endEmpty:
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
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: point-in-time (begin unset, end %s) not aligned with first phase begin (%s)",
					v.Name, ph.Name, ph.EndDate, first.phase.StartDate))
			} else {
				if pt.index > last.index && ph.StartDate == last.phase.EndDate {
					continue
				}
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: point-in-time (begin %s, end unset) not aligned with last phase end (%s)",
					v.Name, ph.Name, ph.StartDate, last.phase.EndDate))
			}
		}
	}
	return reasons
}

// FilterIncompletePhases removes phases where either date is empty from all versions.
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
		return []string{"package has no versions"}
	}
	return nil
}

// ValidateVersionNames checks that all version names match MAJOR.MINOR format.
func ValidateVersionNames(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		if !plcc.MajorMinorRegex.MatchString(v.Name) {
			reasons = append(reasons, fmt.Sprintf("version name %q is not MAJOR.MINOR", v.Name))
		}
	}
	return reasons
}

// ValidatePhases checks that all phases have non-empty dates, end > begin, and
// consecutive phases are continuous (each starts one day after the previous ends).
func ValidatePhases(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		if len(v.Phases) == 0 {
			reasons = append(reasons, fmt.Sprintf("version %q: no phases", v.Name))
			continue
		}

		var validPhases []Phase
		for _, ph := range v.Phases {
			if ph.StartDate == "" {
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: missing begin date", v.Name, ph.Name))
			}
			if ph.EndDate == "" {
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: missing end date", v.Name, ph.Name))
			}
			if ph.StartDate == "" || ph.EndDate == "" {
				continue
			}
			begin, errB := time.Parse("2006-01-02", ph.StartDate)
			end, errE := time.Parse("2006-01-02", ph.EndDate)
			if errB != nil || errE != nil {
				continue
			}
			if !end.After(begin) {
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: end (%s) is not after begin (%s)", v.Name, ph.Name, ph.EndDate, ph.StartDate))
				continue
			}
			validPhases = append(validPhases, ph)
		}

		for i := 1; i < len(validPhases); i++ {
			prevEnd, _ := time.Parse("2006-01-02", validPhases[i-1].EndDate)
			currBegin, _ := time.Parse("2006-01-02", validPhases[i].StartDate)
			expectedBegin := prevEnd.AddDate(0, 0, 1)
			if !currBegin.Equal(expectedBegin) {
				reasons = append(reasons, fmt.Sprintf("version %q phase %q: begin (%s) must be one day after previous phase %q end (%s)",
					v.Name, validPhases[i].Name, validPhases[i].StartDate, validPhases[i-1].Name, validPhases[i-1].EndDate))
			}
		}
	}
	return reasons
}

// ValidateOCPCompatibility checks that all OCP platform versions match MAJOR.MINOR format.
func ValidateOCPCompatibility(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, platform := range v.PlatformCompatibility {
			if platform.Name != "openshift" {
				continue
			}
			for _, ver := range platform.Versions {
				if !plcc.MajorMinorRegex.MatchString(ver) {
					reasons = append(reasons, fmt.Sprintf("version %q: OCP compatibility %q is not MAJOR.MINOR", v.Name, ver))
				}
			}
		}
	}
	return reasons
}
