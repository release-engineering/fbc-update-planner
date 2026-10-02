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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
	goyaml "sigs.k8s.io/yaml/goyaml.v2"
)

// RawMessage distinguishes an absent selection (all) from an explicit null or
// empty selection (invalid), without adding YAML concerns to the assessment API.
type reportConfig struct {
	Selected json.RawMessage `json:"selected"`
	Skipped  []skipGroup     `json:"skipped"`
}

type skipGroup struct {
	Reason    string   `json:"reason"`
	Operators []string `json:"operators"`
}

func readConfig(path string) ([]string, map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read configuration: %w", err)
	}
	names, skips, err := parseConfig(data)
	if err != nil {
		return nil, nil, fmt.Errorf("configuration %s: %w", path, err)
	}
	return names, skips, nil
}

func parseConfig(data []byte) ([]string, map[string]string, error) {
	// YAMLToJSONStrict reads only the first document. Reject additional
	// documents so a second selection or skip policy cannot be silently lost.
	documents := goyaml.NewDecoder(bytes.NewReader(data))
	var document any
	if err := documents.Decode(&document); err != nil {
		return nil, nil, fmt.Errorf("read YAML document: %w", err)
	}
	if err := documents.Decode(&document); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("expected a single YAML document")
	}
	// Convert first, then decode strict JSON: YAML's typed unmarshaller can
	// otherwise coerce numeric and boolean values into operator names or notes.
	encoded, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		return nil, nil, err
	}
	var config *reportConfig
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, nil, err
	}
	if config == nil {
		return nil, nil, fmt.Errorf("expected a configuration mapping")
	}
	var names []string
	if config.Selected != nil {
		var selected []string
		if err := json.Unmarshal(config.Selected, &selected); err != nil {
			return nil, nil, fmt.Errorf("selected: %w", err)
		}
		if len(selected) == 0 {
			return nil, nil, fmt.Errorf("selected must contain at least one operator; omit it to select all")
		}
		for _, value := range selected {
			name := strings.TrimSpace(value)
			if name == "" {
				return nil, nil, fmt.Errorf("selected contains an empty operator name")
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	reasons := make(map[string]string)
	for i, group := range config.Skipped {
		reason := strings.TrimSpace(group.Reason)
		if reason == "" || len(group.Operators) == 0 {
			return nil, nil, fmt.Errorf("skipped group %d requires a nonempty reason and operators list", i+1)
		}
		for _, value := range group.Operators {
			name := strings.TrimSpace(value)
			if name == "" {
				return nil, nil, fmt.Errorf("skipped group %d contains an empty operator name", i+1)
			}
			if _, exists := reasons[name]; exists {
				return nil, nil, fmt.Errorf("operator %q appears more than once in skipped groups", name)
			}
			reasons[name] = reason
		}
	}
	return names, reasons, nil
}
