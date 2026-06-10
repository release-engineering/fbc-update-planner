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
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNewPackageWriter(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantType  reflect.Type
		wantError bool
	}{
		{name: "json", format: "json", wantType: reflect.TypeFor[JSONWriter]()},
		{name: "json-pretty", format: "json-pretty", wantType: reflect.TypeFor[JSONPrettyWriter]()},
		{name: "yaml", format: "yaml", wantType: reflect.TypeFor[YAMLWriter]()},
		{name: "invalid format", format: "xml", wantError: true},
		{name: "empty format", format: "", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := NewPackageWriter(tt.format)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := reflect.TypeOf(w); got != tt.wantType {
				t.Errorf("writer type = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func samplePackage(name string) *Package {
	return &Package{
		Schema: Schema,
		Name:   name,
		Versions: []Version{{
			Name: "1.0",
			Phases: []Phase{{
				Name:      "Full support",
				StartDate: "2025-01-01",
				EndDate:   "2025-06-30",
			}},
		}},
	}
}

func TestWriters(t *testing.T) {
	jsonSingle := `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"test-op","versions":[{"name":"1.0","phases":[{"name":"Full support","startDate":"2025-01-01","endDate":"2025-06-30"}]}]}` + "\n"
	jsonMulti := `[{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"op-a","versions":[{"name":"1.0","phases":[{"name":"Full support","startDate":"2025-01-01","endDate":"2025-06-30"}]}]},{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"op-b","versions":[{"name":"1.0","phases":[{"name":"Full support","startDate":"2025-01-01","endDate":"2025-06-30"}]}]}]` + "\n"

	prettySingle := strings.Join([]string{
		`{`,
		`  "schema": "io.openshift.operators.lifecycles.v1alpha1",`,
		`  "package": "test-op",`,
		`  "versions": [`,
		`    {`,
		`      "name": "1.0",`,
		`      "phases": [`,
		`        {`,
		`          "name": "Full support",`,
		`          "startDate": "2025-01-01",`,
		`          "endDate": "2025-06-30"`,
		`        }`,
		`      ]`,
		`    }`,
		`  ]`,
		`}`,
		``,
	}, "\n")

	prettyMulti := strings.Join([]string{
		`[`,
		`  {`,
		`    "schema": "io.openshift.operators.lifecycles.v1alpha1",`,
		`    "package": "test-op",`,
		`    "versions": [`,
		`      {`,
		`        "name": "1.0",`,
		`        "phases": [`,
		`          {`,
		`            "name": "Full support",`,
		`            "startDate": "2025-01-01",`,
		`            "endDate": "2025-06-30"`,
		`          }`,
		`        ]`,
		`      }`,
		`    ]`,
		`  },`,
		`  {`,
		`    "schema": "io.openshift.operators.lifecycles.v1alpha1",`,
		`    "package": "op-b",`,
		`    "versions": [`,
		`      {`,
		`        "name": "1.0",`,
		`        "phases": [`,
		`          {`,
		`            "name": "Full support",`,
		`            "startDate": "2025-01-01",`,
		`            "endDate": "2025-06-30"`,
		`          }`,
		`        ]`,
		`      }`,
		`    ]`,
		`  }`,
		`]`,
		``,
	}, "\n")

	yamlSingle := strings.Join([]string{
		`package: test-op`,
		`schema: io.openshift.operators.lifecycles.v1alpha1`,
		`versions:`,
		`- name: "1.0"`,
		`  phases:`,
		`  - endDate: "2025-06-30"`,
		`    name: Full support`,
		`    startDate: "2025-01-01"`,
		``,
	}, "\n")

	yamlMulti := strings.Join([]string{
		`package: op-a`,
		`schema: io.openshift.operators.lifecycles.v1alpha1`,
		`versions:`,
		`- name: "1.0"`,
		`  phases:`,
		`  - endDate: "2025-06-30"`,
		`    name: Full support`,
		`    startDate: "2025-01-01"`,
		`---`,
		`package: op-b`,
		`schema: io.openshift.operators.lifecycles.v1alpha1`,
		`versions:`,
		`- name: "1.0"`,
		`  phases:`,
		`  - endDate: "2025-06-30"`,
		`    name: Full support`,
		`    startDate: "2025-01-01"`,
		``,
	}, "\n")

	tests := []struct {
		name     string
		writer   PackageWriter
		packages []*Package
		want     string
	}{
		// JSONWriter
		{name: "json/zero packages", writer: JSONWriter{}, packages: nil, want: ""},
		{name: "json/single package", writer: JSONWriter{}, packages: []*Package{samplePackage("test-op")}, want: jsonSingle},
		{name: "json/multiple packages", writer: JSONWriter{}, packages: []*Package{samplePackage("op-a"), samplePackage("op-b")}, want: jsonMulti},
		// JSONPrettyWriter
		{name: "json-pretty/zero packages", writer: JSONPrettyWriter{}, packages: nil, want: ""},
		{name: "json-pretty/single package", writer: JSONPrettyWriter{}, packages: []*Package{samplePackage("test-op")}, want: prettySingle},
		{name: "json-pretty/multiple packages", writer: JSONPrettyWriter{}, packages: []*Package{samplePackage("test-op"), samplePackage("op-b")}, want: prettyMulti},
		// YAMLWriter
		{name: "yaml/zero packages", writer: YAMLWriter{}, packages: nil, want: ""},
		{name: "yaml/single package", writer: YAMLWriter{}, packages: []*Package{samplePackage("test-op")}, want: yamlSingle},
		{name: "yaml/multiple packages", writer: YAMLWriter{}, packages: []*Package{samplePackage("op-a"), samplePackage("op-b")}, want: yamlMulti},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tt.writer.Write(&buf, tt.packages...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriterErrorPaths(t *testing.T) {
	pkg := samplePackage("test-op")
	writers := []struct {
		name   string
		writer PackageWriter
	}{
		{"JSONWriter", JSONWriter{}},
		{"JSONPrettyWriter", JSONPrettyWriter{}},
		{"YAMLWriter", YAMLWriter{}},
	}

	for _, ww := range writers {
		t.Run(ww.name, func(t *testing.T) {
			err := ww.writer.Write(errWriter{}, pkg)
			if err == nil {
				t.Fatal("expected error writing to failing writer")
			}
			if !strings.Contains(err.Error(), "write failed") {
				t.Errorf("error = %q, want it to contain %q", err.Error(), "write failed")
			}
		})
	}
}
