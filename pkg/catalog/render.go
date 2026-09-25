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

// Package catalog reads the JSON object stream produced by opm render.
package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
)

var bundleVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(?:\.([0-9]+))?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// Data is the normalized catalog view used by coverage and action reports.
type Data struct {
	// LifecyclePackages includes entries with no versions.
	LifecyclePackages map[string]bool
	// LifecycleVersions holds MAJOR.MINOR version sets by package.
	LifecycleVersions map[string]map[string]bool
	// BundleVersions holds normalized MAJOR.MINOR bundle version sets.
	BundleVersions map[string]map[string]bool
}

type renderObject struct {
	Schema   string `json:"schema"`
	Package  string `json:"package"`
	Versions []struct {
		Name string `json:"name"`
	} `json:"versions"`
	Properties []struct {
		Type  string `json:"type"`
		Value struct {
			Version string `json:"version"`
		} `json:"value"`
	} `json:"properties"`
}

// ParseRender consumes opm's JSON object stream into version sets. The
// presence of a lifecycle entry is recorded even if it has no versions.
func ParseRender(r io.Reader) (*Data, error) {
	data := &Data{
		LifecyclePackages: make(map[string]bool),
		LifecycleVersions: make(map[string]map[string]bool),
		BundleVersions:    make(map[string]map[string]bool),
	}
	decoder := json.NewDecoder(r)
	for {
		var obj renderObject
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				return data, nil
			}
			return nil, fmt.Errorf("decoding opm render output: %w", err)
		}
		if obj.Package == "" {
			continue
		}
		switch obj.Schema {
		case fbc.Schema:
			data.LifecyclePackages[obj.Package] = true
			if data.LifecycleVersions[obj.Package] == nil {
				data.LifecycleVersions[obj.Package] = make(map[string]bool)
			}
			for _, version := range obj.Versions {
				if version.Name == "" {
					continue
				}
				parsed, err := fbc.ParseMajorMinor(version.Name)
				if err != nil {
					return nil, fmt.Errorf("lifecycle package %q: %w", obj.Package, err)
				}
				data.LifecycleVersions[obj.Package][parsed.String()] = true
			}
		case "olm.bundle":
			for _, property := range obj.Properties {
				if property.Type != "olm.package" || property.Value.Version == "" {
					continue
				}
				matches := bundleVersionPattern.FindStringSubmatch(property.Value.Version)
				if len(matches) == 0 {
					return nil, fmt.Errorf("bundle package %q: invalid version %q", obj.Package, property.Value.Version)
				}
				parsed, err := fbc.ParseMajorMinor(matches[1] + "." + matches[2])
				if err != nil {
					return nil, fmt.Errorf("bundle package %q: %w", obj.Package, err)
				}
				if data.BundleVersions[obj.Package] == nil {
					data.BundleVersions[obj.Package] = make(map[string]bool)
				}
				data.BundleVersions[obj.Package][parsed.String()] = true
			}
		}
	}
}
