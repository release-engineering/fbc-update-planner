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
	"encoding/json"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"
)

// PackageWriter serializes FBC packages to a writer.
type PackageWriter interface {
	Write(out io.Writer, packages ...*Package) error
	Ext() string
}

// NewPackageWriter returns a PackageWriter for the given format ("json", "json-pretty", or "yaml").
func NewPackageWriter(format string) (PackageWriter, error) {
	switch format {
	case "json":
		return JSONWriter{}, nil
	case "json-pretty":
		return JSONPrettyWriter{}, nil
	case "yaml":
		return YAMLWriter{}, nil
	default:
		return nil, fmt.Errorf("unsupported format %q: must be \"json\", \"json-pretty\", or \"yaml\"", format)
	}
}

// JSONWriter writes packages as compact JSON. A single package is written
// directly; multiple packages are wrapped in an array.
type JSONWriter struct{}

func (JSONWriter) Ext() string { return "json" }

func (JSONWriter) Write(out io.Writer, packages ...*Package) error {
	if len(packages) == 0 {
		return nil
	}
	var v any = packages
	if len(packages) == 1 {
		v = packages[0]
	}
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')
	if _, err := out.Write(jsonBytes); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}

// JSONPrettyWriter writes packages as indented JSON. A single package is written
// directly; multiple packages are wrapped in an array.
type JSONPrettyWriter struct{}

func (JSONPrettyWriter) Ext() string { return "json" }

func (JSONPrettyWriter) Write(out io.Writer, packages ...*Package) error {
	if len(packages) == 0 {
		return nil
	}
	var v any = packages
	if len(packages) == 1 {
		v = packages[0]
	}
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')
	if _, err := out.Write(jsonBytes); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}

// YAMLWriter writes packages as YAML documents separated by "---".
type YAMLWriter struct{}

func (YAMLWriter) Ext() string { return "yaml" }

func (YAMLWriter) Write(out io.Writer, packages ...*Package) error {
	for i, pkg := range packages {
		yamlBytes, err := yaml.Marshal(pkg)
		if err != nil {
			return fmt.Errorf("marshaling YAML: %w", err)
		}
		if i > 0 {
			yamlBytes = append([]byte("---\n"), yamlBytes...)
		}
		if _, err := out.Write(yamlBytes); err != nil {
			return fmt.Errorf("writing YAML: %w", err)
		}
	}
	return nil
}
