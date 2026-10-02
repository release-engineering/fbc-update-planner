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

package plcccheck

import (
	"fmt"
	"strings"
)

// ApplySkips replaces the reporting exceptions on this assessment. It preserves
// all evidence and pipeline outputs, and ignores names outside the assessed set.
// Call before NewReport. Invalid policy leaves the assessment unchanged.
func (a *Assessment) ApplySkips(reasons map[string]string) error {
	for name, reason := range reasons {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("skip policy requires a nonempty operator name and reason for %q", name)
		}
	}
	for i := range a.Packages {
		pkg := &a.Packages[i]
		pkg.SkipReason = reasons[pkg.Name]
		pkg.Action = pkg.primaryAction()
		if pkg.SkipReason != "" {
			pkg.Action = Skipped
		}
	}
	return nil
}
