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
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
)

// LifecycleDifference describes one differing field or missing list entry.
// PLCC contains the generated FBC value, Catalog the shipped value. A nil raw
// message means absent; an explicit JSON null is retained as the bytes "null".
// Path addresses normalized records, using names for versions and platforms
// and zero-based indexes for ordered lists and duplicate occurrences.
type LifecycleDifference struct {
	Path    string          `json:"path"`
	PLCC    json.RawMessage `json:"plcc,omitempty"`
	Catalog json.RawMessage `json:"catalog,omitempty"`
}

// lifecycleDifferences compares all generated and shipped records for a package.
// Record and unordered list order is ignored, but multiplicity is preserved.
// Catalog records are intentionally not decoded into fbc.Package: doing so
// would discard unknown fields and conflate omitted, null, and empty values.
func lifecycleDifferences(generated []*fbc.Package, shipped []json.RawMessage) ([]LifecycleDifference, error) {
	expected, err := canonicalLifecycles(generated)
	if err != nil {
		return nil, err
	}
	actual, err := canonicalLifecycles(shipped)
	if err != nil {
		return nil, err
	}
	var differences []LifecycleDifference
	diffLifecycleList("lifecycles", expected, actual, recordNode, &differences)
	return differences, nil
}

// Only these schema locations contain unordered named lists. Unknown arrays
// and phases remain ordered, even when their elements happen to have names.
type lifecycleNode int

const (
	otherNode lifecycleNode = iota
	recordNode
	versionNode
	platformNode
)

func diffLifecycleValue(path string, expected, actual json.RawMessage, node lifecycleNode, differences *[]LifecycleDifference) {
	if bytes.Equal(expected, actual) {
		return
	}
	var expectedObject, actualObject map[string]json.RawMessage
	if json.Unmarshal(expected, &expectedObject) == nil && expectedObject != nil &&
		json.Unmarshal(actual, &actualObject) == nil && actualObject != nil {
		keys := make([]string, 0, len(expectedObject)+len(actualObject))
		for key := range expectedObject {
			keys = append(keys, key)
		}
		for key := range actualObject {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			childPath := path + "[" + strconv.Quote(key) + "]"
			// Use familiar dotted paths for the schema's ordinary field names;
			// quoted brackets keep arbitrary unknown keys unambiguous.
			if simpleLifecycleField(key) {
				childPath = path + "." + key
			}
			expectedValue, actualValue := expectedObject[key], actualObject[key]
			childNode := otherNode
			if node == recordNode && key == "versions" {
				childNode = versionNode
			} else if node == versionNode && key == "platformCompatibility" {
				childNode = platformNode
			}
			var expectedList, actualList []json.RawMessage
			if childNode != otherNode && json.Unmarshal(expectedValue, &expectedList) == nil && expectedList != nil &&
				json.Unmarshal(actualValue, &actualList) == nil && actualList != nil {
				diffNamedLifecycleList(childPath, expectedList, actualList, childNode, differences)
			} else if node == platformNode && key == "versions" {
				// Compatible platform versions are already sorted. Show the
				// complete list so additions/removals do not shift other values.
				if !bytes.Equal(expectedValue, actualValue) {
					*differences = append(*differences, LifecycleDifference{Path: childPath, PLCC: expectedValue, Catalog: actualValue})
				}
			} else {
				diffLifecycleValue(childPath, expectedValue, actualValue, otherNode, differences)
			}
		}
		return
	}
	var expectedList, actualList []json.RawMessage
	if json.Unmarshal(expected, &expectedList) == nil && expectedList != nil &&
		json.Unmarshal(actual, &actualList) == nil && actualList != nil {
		for i := range max(len(expectedList), len(actualList)) {
			var expectedValue, actualValue json.RawMessage
			if i < len(expectedList) {
				expectedValue = expectedList[i]
			}
			if i < len(actualList) {
				actualValue = actualList[i]
			}
			diffLifecycleValue(fmt.Sprintf("%s[%d]", path, i), expectedValue, actualValue, otherNode, differences)
		}
		return
	}
	*differences = append(*differences, LifecycleDifference{Path: path, PLCC: expected, Catalog: actual})
}

func simpleLifecycleField(key string) bool {
	if key == "" {
		return false
	}
	for _, ch := range key {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

func diffNamedLifecycleList(path string, expected, actual []json.RawMessage, node lifecycleNode, differences *[]LifecycleDifference) {
	expectedGroups, actualGroups := namedLifecycleGroups(expected), namedLifecycleGroups(actual)
	if expectedGroups == nil || actualGroups == nil {
		// Malformed entries still produce evidence. Canonical ordering and
		// exact matches keep unchanged entries out of the diff.
		diffLifecycleList(path, expected, actual, node, differences)
		return
	}
	var names []string
	for name := range expectedGroups {
		names = append(names, name)
	}
	for name := range actualGroups {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		childPath := path + "[name=" + strconv.Quote(name) + "]"
		expectedValues, actualValues := expectedGroups[name], actualGroups[name]
		if max(len(expectedValues), len(actualValues)) > 1 {
			diffLifecycleList(childPath, expectedValues, actualValues, node, differences)
			continue
		}
		var expectedValue, actualValue json.RawMessage
		if len(expectedValues) > 0 {
			expectedValue = expectedValues[0]
		}
		if len(actualValues) > 0 {
			actualValue = actualValues[0]
		}
		diffLifecycleValue(childPath, expectedValue, actualValue, node, differences)
	}
}

func namedLifecycleGroups(values []json.RawMessage) map[string][]json.RawMessage {
	groups := make(map[string][]json.RawMessage)
	for _, value := range values {
		var entry map[string]json.RawMessage
		var name string
		if json.Unmarshal(value, &entry) != nil || json.Unmarshal(entry["name"], &name) != nil || string(entry["name"]) == "null" {
			return nil
		}
		groups[name] = append(groups[name], value)
	}
	return groups
}

// Match equal records or duplicate entries first, then pair remaining values
// in canonical order. This preserves multiplicity without showing differences
// in records that are present unchanged on both sides.
func diffLifecycleList(path string, expected, actual []json.RawMessage, node lifecycleNode, differences *[]LifecycleDifference) {
	matched := make([]bool, len(actual))
	var pending []int
	for i, value := range expected {
		found := false
		for j, candidate := range actual {
			if !matched[j] && bytes.Equal(value, candidate) {
				matched[j], found = true, true
				break
			}
		}
		if !found {
			pending = append(pending, i)
		}
	}
	var remaining []json.RawMessage
	for i, value := range actual {
		if !matched[i] {
			remaining = append(remaining, value)
		}
	}
	for i := range max(len(pending), len(remaining)) {
		index := len(expected) + i - len(pending)
		var expectedValue, actualValue json.RawMessage
		if i < len(pending) {
			index = pending[i]
			expectedValue = expected[index]
		}
		if i < len(remaining) {
			actualValue = remaining[i]
		}
		diffLifecycleValue(fmt.Sprintf("%s[%d]", path, index), expectedValue, actualValue, node, differences)
	}
}

func canonicalLifecycles(input any) ([]json.RawMessage, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&records); err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(records))
	for _, record := range records {
		versions, _ := record["versions"].([]any)
		for _, value := range versions {
			version, _ := value.(map[string]any)
			platforms, _ := version["platformCompatibility"].([]any)
			for _, value := range platforms {
				platform, _ := value.(map[string]any)
				if err := sortJSONField(platform, "versions"); err != nil {
					return nil, err
				}
			}
			if err := sortJSONField(version, "platformCompatibility"); err != nil {
				return nil, err
			}
		}
		if err := sortJSONField(record, "versions"); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		result = append(result, encoded)
	}
	slices.SortFunc(result, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
	return result, nil
}

// sortJSONField only sorts arrays at known unordered schema fields. Malformed
// field types, absent fields, and nulls are preserved for comparison. Phases
// and arrays inside unknown fields keep their original order.
func sortJSONField(object map[string]any, field string) error {
	values, ok := object[field].([]any)
	if !ok {
		return nil
	}
	encoded := make([]json.RawMessage, len(values))
	for i, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		encoded[i] = data
	}
	slices.SortFunc(encoded, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
	object[field] = encoded
	return nil
}
