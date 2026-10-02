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

package catalog

import (
	"encoding/json"
	"fmt"
	"io"
)

const lifecycleSchema = "io.openshift.operators.lifecycles.v1alpha1"

// Inventory contains packages found in bundle or lifecycle records. The map
// keys are package names. The caller owns the returned inventory and its slices.
type Inventory struct {
	Packages map[string]Package
}

// Package records the bundle and lifecycle metadata needed for coverage checks.
// Slices retain encounter order and duplicates; versions are not normalized.
type Package struct {
	Bundles []Bundle
	// HasLifecycle distinguishes an empty lifecycle entry from an absent one.
	HasLifecycle      bool
	LifecycleVersions []string
}

// Bundle identifies a catalog bundle and its original olm.package version.
type Bundle struct {
	Name    string
	Version string
}

// Parse reads a stream of JSON catalog objects, as produced by opm render.
// It reads bundle and lifecycle metadata and ignores other schemas and unused
// fields. Lifecycle records without a package name are ignored for compatibility
// with the existing reporting script. Empty input yields an empty inventory.
//
// Consumed metadata must have the expected JSON types and required nonempty
// strings. Version syntax, lifecycle contents, and coverage are not validated.
// Any read or parsing error returns a nil inventory, never partial results.
func Parse(r io.Reader) (*Inventory, error) {
	result := &Inventory{Packages: make(map[string]Package)}
	decoder := json.NewDecoder(r)
	for index := 1; ; index++ {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				return result, nil
			}
			return nil, fmt.Errorf("catalog record %d: %w", index, err)
		}
		var header struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, fmt.Errorf("catalog record %d: %w", index, err)
		}
		if header.Schema == "" {
			return nil, fmt.Errorf("catalog record %d: missing or empty schema", index)
		}
		var err error
		switch header.Schema {
		case "olm.bundle":
			err = result.addBundle(raw)
		case lifecycleSchema:
			err = result.addLifecycle(raw)
		}
		if err != nil {
			return nil, fmt.Errorf("catalog record %d (%s): %w", index, header.Schema, err)
		}
	}
}

func (in *Inventory) addBundle(raw json.RawMessage) error {
	var record struct {
		Package    string `json:"package"`
		Name       string `json:"name"`
		Properties []struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return fmt.Errorf("package %q bundle %q: %w", record.Package, record.Name, err)
	}
	if record.Package == "" || record.Name == "" {
		return fmt.Errorf("package %q bundle %q: package and name must be nonempty", record.Package, record.Name)
	}
	var value json.RawMessage
	count := 0
	for _, property := range record.Properties {
		if property.Type == "olm.package" {
			count++
			value = property.Value
		}
	}
	if count != 1 {
		return fmt.Errorf("package %q bundle %q: expected exactly one olm.package property, found %d", record.Package, record.Name, count)
	}
	var property struct {
		PackageName string `json:"packageName"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(value, &property); err != nil {
		return fmt.Errorf("package %q bundle %q: olm.package property: %w", record.Package, record.Name, err)
	}
	if property.PackageName != record.Package {
		return fmt.Errorf("package %q bundle %q: olm.package packageName %q does not match package", record.Package, record.Name, property.PackageName)
	}
	if property.Version == "" {
		return fmt.Errorf("package %q bundle %q: olm.package version must be nonempty", record.Package, record.Name)
	}
	pkg := in.Packages[record.Package]
	pkg.Bundles = append(pkg.Bundles, Bundle{Name: record.Name, Version: property.Version})
	in.Packages[record.Package] = pkg
	return nil
}

func (in *Inventory) addLifecycle(raw json.RawMessage) error {
	var record struct {
		Package  string          `json:"package"`
		Versions json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return fmt.Errorf("package %q: %w", record.Package, err)
	}
	if record.Package == "" {
		return nil
	}
	var versions []struct {
		Name string `json:"name"`
	}
	if len(record.Versions) > 0 {
		if err := json.Unmarshal(record.Versions, &versions); err != nil {
			return fmt.Errorf("package %q: lifecycle versions: %w", record.Package, err)
		}
	}
	pkg := in.Packages[record.Package]
	pkg.HasLifecycle = true
	for i, version := range versions {
		if version.Name == "" {
			return fmt.Errorf("package %q: lifecycle version %d: name must be nonempty", record.Package, i+1)
		}
		pkg.LifecycleVersions = append(pkg.LifecycleVersions, version.Name)
	}
	in.Packages[record.Package] = pkg
	return nil
}
