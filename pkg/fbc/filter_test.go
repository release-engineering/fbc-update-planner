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
	"testing"
)

func TestFilter(t *testing.T) {
	var callOrder []string
	f1 := func(p *Package) []string {
		callOrder = append(callOrder, "f1")
		return nil
	}
	f2 := func(p *Package) []string {
		callOrder = append(callOrder, "f2")
		return []string{"rejected"}
	}
	f3 := func(p *Package) []string {
		callOrder = append(callOrder, "f3")
		return nil
	}

	pkg := &Package{Name: "test"}
	reasons := pkg.Filter(f1, f2, f3)

	if len(reasons) != 1 || reasons[0] != "rejected" {
		t.Errorf("expected rejection from f2, got %v", reasons)
	}
	if len(callOrder) != 2 || callOrder[0] != "f1" || callOrder[1] != "f2" {
		t.Errorf("expected short-circuit after f2, call order = %v", callOrder)
	}
}

func TestFilterIncompletePhases(t *testing.T) {
	pkg := &Package{Versions: []Version{{
		Name: "1.0",
		Phases: []Phase{
			{Name: "GA", StartDate: "", EndDate: "2025-01-01"},
			{Name: "Full support", StartDate: "2025-01-01", EndDate: "2025-12-31"},
			{Name: "EOL", StartDate: "2025-12-31", EndDate: ""},
		},
	}}}

	reasons := FilterIncompletePhases(pkg)

	if len(reasons) != 0 {
		t.Errorf("expected no reasons, got %v", reasons)
	}
	if len(pkg.Versions[0].Phases) != 1 {
		t.Fatalf("expected 1 phase kept, got %d", len(pkg.Versions[0].Phases))
	}
	if pkg.Versions[0].Phases[0].Name != "Full support" {
		t.Errorf("expected 'Full support' kept, got %q", pkg.Versions[0].Phases[0].Name)
	}
}

