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

package report

import (
	"encoding/json"
	"fmt"
	"io"
)

// ValidationResult records the outcome of validating a package or version.
type ValidationResult struct {
	PackageName string   `json:"packageName"`
	Version     string   `json:"version,omitempty"`
	Valid       bool     `json:"valid"`
	Reasons     []string `json:"reasons,omitempty"`
}

// LogResults writes validation results as JSON lines to the given writer.
func LogResults(w io.Writer, results ...ValidationResult) error {
	enc := json.NewEncoder(w)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("failed to write validation log: %w", err)
		}
	}
	return nil
}
