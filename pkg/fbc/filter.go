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

// Filter is a pipeline callback that can mutate a Package.
// A non-empty return means the package should be rejected.
type Filter func(*Package) []string

// DefaultFilters returns the standard processing pipeline for FBC output cleanup.
func DefaultFilters() []Filter {
	return []Filter{
		FilterIncompletePhases,
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
