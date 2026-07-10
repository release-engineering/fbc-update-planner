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

import "fmt"

// Filter is a pipeline callback that can mutate a Package.
// A non-empty return means the package should be rejected.
type Filter func(*Package) []string

type filterEntry struct {
	Label  string
	Group  string // "filter" or "invariant"
	Filter Filter
}

var filterRegistry = []filterEntry{
	// Mutation filters run first, followed by invariant validators.
	{"FBC-MUTATE-01", "filter", FilterIncompletePhases},
	// Invariants required by FBC consumers. These must not be relaxed.
	{"FBC-VAL-01", "invariant", ValidatePackageHasVersions},
	{"FBC-VAL-02", "invariant", ValidateVersionsHavePhases},
	{"FBC-VAL-03", "invariant", ValidatePhaseDates},
	{"FBC-VAL-04", "invariant", ValidateDateOrdering},
	{"FBC-VAL-05", "invariant", ValidatePhaseContiguity},
}

// DefaultFilters returns the standard processing pipeline for FBC output cleanup.
func DefaultFilters() []Filter {
	result := make([]Filter, len(filterRegistry))
	for i, entry := range filterRegistry {
		result[i] = entry.Filter
	}
	return result
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

// FilterIncompletePhases removes phases where either date is nil from all versions.
func FilterIncompletePhases(p *Package) []string {
	for i := range p.Versions {
		filtered := p.Versions[i].Phases[:0]
		for _, ph := range p.Versions[i].Phases {
			if ph.StartDate != nil && ph.EndDate != nil {
				filtered = append(filtered, ph)
			}
		}
		p.Versions[i].Phases = filtered
	}
	return nil
}

// ValidatePackageHasVersions rejects a package that has no versions.
// FBC-VAL-01
func ValidatePackageHasVersions(p *Package) []string {
	if len(p.Versions) == 0 {
		return []string{"FBC-VAL-01: package has no versions"}
	}
	return nil
}

// ValidateVersionsHavePhases rejects a package if any version has no phases.
// FBC-VAL-02
func ValidateVersionsHavePhases(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		if len(v.Phases) == 0 {
			reasons = append(reasons, fmt.Sprintf("FBC-VAL-02: version %s has no phases", v.Name))
		}
	}
	return reasons
}

// ValidatePhaseDates rejects a package if any phase has a nil start or end date.
// FBC-VAL-03
func ValidatePhaseDates(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, ph := range v.Phases {
			if ph.StartDate == nil {
				reasons = append(reasons, fmt.Sprintf("FBC-VAL-03: version %s phase %q has nil start date", v.Name, ph.Name))
			}
			if ph.EndDate == nil {
				reasons = append(reasons, fmt.Sprintf("FBC-VAL-03: version %s phase %q has nil end date", v.Name, ph.Name))
			}
		}
	}
	return reasons
}

// ValidateDateOrdering rejects a package if any phase has a start date after its end date.
// Point-in-time phases (nil start or end date) are ignored.
// FBC-VAL-04
func ValidateDateOrdering(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, ph := range v.Phases {
			if ph.StartDate == nil || ph.EndDate == nil {
				continue
			}
			if ph.StartDate.Compare(*ph.EndDate) > 0 {
				reasons = append(reasons, fmt.Sprintf(
					"FBC-VAL-04: version %s phase %q start date %s is after end date %s",
					v.Name, ph.Name, ph.StartDate, ph.EndDate,
				))
			}
		}
	}
	return reasons
}

// ValidatePhaseContiguity rejects a package if phases within any version are
// not contiguous. Phases are contiguous when the end date of each phase is
// exactly the day before the start date of the next phase.
// Point-in-time phases (nil start or end date) are ignored.
// FBC-VAL-05
func ValidatePhaseContiguity(p *Package) []string {
	var reasons []string
	for _, v := range p.Versions {
		for i := 0; i+1 < len(v.Phases); i++ {
			cur := v.Phases[i]
			next := v.Phases[i+1]
			if cur.EndDate == nil || next.StartDate == nil {
				continue
			}
			if cur.EndDate.NextDay().Compare(*next.StartDate) != 0 {
				reasons = append(reasons, fmt.Sprintf(
					"FBC-VAL-05: version %s: gap or overlap between phase %q (ends %s) and phase %q (starts %s)",
					v.Name, cur.Name, cur.EndDate, next.Name, next.StartDate,
				))
			}
		}
	}
	return reasons
}
