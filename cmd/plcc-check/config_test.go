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

package main

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestReportConfig(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		selected    []string
		skips       map[string]string
		wantError   string
	}{
		{name: "all", input: "skipped: []", skips: map[string]string{}},
		{name: "empty mapping", input: "{}", skips: map[string]string{}},
		{name: "selection and groups", input: `selected: [" mixed ", full, mixed]
skipped:
  - reason: " Shared reason "
    operators: [mixed, outside-selection]
  - reason: "Individual note"
    operators: [full]
`, selected: []string{"mixed", "full"}, skips: map[string]string{"mixed": "Shared reason", "outside-selection": "Shared reason", "full": "Individual note"}},
		{name: "empty input", input: "", wantError: "YAML document"},
		{name: "null", input: "null", wantError: "mapping"},
		{name: "list", input: "[]", wantError: "unmarshal"},
		{name: "malformed", input: "selected: [", wantError: "YAML"},
		{name: "multiple documents", input: "{}\n---\nselected: [full]", wantError: "single YAML"},
		{name: "unknown field", input: "select: [full]", wantError: "unknown field"},
		{name: "unknown group field", input: "skipped: [{reason: note, operators: [full], typo: x}]", wantError: "unknown field"},
		{name: "duplicate key", input: "selected: [full]\nselected: [mixed]", wantError: "already set"},
		{name: "empty selection", input: "selected: []", wantError: "at least one"},
		{name: "null selection", input: "selected: null", wantError: "at least one"},
		{name: "blank selected name", input: "selected: ['  ']", wantError: "empty operator"},
		{name: "null selected name", input: "selected: [null]", wantError: "empty operator"},
		{name: "numeric selected name", input: "selected: [123]", wantError: "unmarshal"},
		{name: "missing reason", input: "skipped: [{operators: [full]}]", wantError: "nonempty reason"},
		{name: "blank reason", input: "skipped: [{reason: ' ', operators: [full]}]", wantError: "nonempty reason"},
		{name: "numeric reason", input: "skipped: [{reason: 42, operators: [full]}]", wantError: "unmarshal"},
		{name: "empty group", input: "skipped: [{reason: note, operators: []}]", wantError: "operators list"},
		{name: "blank skipped name", input: "skipped: [{reason: note, operators: [' ']}]", wantError: "empty operator"},
		{name: "duplicate within group", input: "skipped: [{reason: note, operators: [full, full]}]", wantError: "more than once"},
		{name: "duplicate across groups", input: "skipped: [{reason: first, operators: [full]}, {reason: second, operators: [full]}]", wantError: "more than once"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			names, skips, err := parseConfig([]byte(tt.input))
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(names, tt.selected) || !reflect.DeepEqual(skips, tt.skips) {
				t.Fatalf("got %v, %v, %v; want %v, %v", names, skips, err, tt.selected, tt.skips)
			}
		})
	}
}

func TestRunConfigErrorsBeforeLoading(t *testing.T) {
	config := writeTestFile(t, t.TempDir(), "config.yaml", "selected: []")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--config", ""}, "--config must not be empty"},
		{[]string{"--config", config, "operators"}, "mutually exclusive"},
		{[]string{"--config", "missing.yaml"}, "read configuration"},
		{[]string{"--config", config}, "selected must contain"},
	} {
		// A nonexistent source ensures configuration errors precede input I/O.
		args := append([]string{"-i", "missing-source.json", "-o", t.TempDir()}, tt.args...)
		if err := run(t.Context(), args, io.Discard); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: error = %v, want %q", tt.args, err, tt.want)
		}
	}
}
