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
	"regexp"
	"slices"
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
)

// Accept canonical bundle semver and the MAJOR.MINOR shorthand understood by
// the previous report. Suffixes require a patch component. Numeric prerelease
// identifiers are checked separately for leading zeros.
var bundleVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func bundleMajorMinor(version string) (fbc.MajorMinor, error) {
	matches := bundleVersionPattern.FindStringSubmatch(version)
	if matches == nil || (matches[3] == "" && (matches[4] != "" || matches[5] != "")) {
		return fbc.MajorMinor{}, fmt.Errorf("invalid bundle version %q; expected MAJOR.MINOR or semantic version", version)
	}
	for _, identifier := range strings.Split(matches[4], ".") {
		if len(identifier) > 1 && identifier[0] == '0' && strings.Trim(identifier, "0123456789") == "" {
			return fbc.MajorMinor{}, fmt.Errorf("invalid bundle version %q: numeric prerelease identifier has a leading zero", version)
		}
	}
	return fbc.ParseMajorMinor(matches[1] + "." + matches[2])
}

func uniqueVersions(versions []fbc.MajorMinor) []fbc.MajorMinor {
	slices.SortFunc(versions, fbc.MajorMinor.Compare)
	return slices.Compact(versions)
}

func coverage(name string, pkg catalog.Package) (*Coverage, error) {
	result := &Coverage{Bundles: slices.Clone(pkg.Bundles), HasLifecycle: pkg.HasLifecycle}
	for _, bundle := range pkg.Bundles {
		version, err := bundleMajorMinor(bundle.Version)
		if err != nil {
			return nil, fmt.Errorf("package %q bundle %q: %w", name, bundle.Name, err)
		}
		result.BundleVersions = append(result.BundleVersions, version)
	}
	for _, raw := range pkg.LifecycleVersions {
		version, err := fbc.ParseMajorMinor(raw)
		if err != nil {
			return nil, fmt.Errorf("package %q lifecycle: %w", name, err)
		}
		result.LifecycleVersions = append(result.LifecycleVersions, version)
	}
	result.BundleVersions = uniqueVersions(result.BundleVersions)
	result.LifecycleVersions = uniqueVersions(result.LifecycleVersions)
	for _, version := range result.BundleVersions {
		if result.HasLifecycle && slices.Contains(result.LifecycleVersions, version) {
			result.Covered++
		}
	}
	return result, nil
}
