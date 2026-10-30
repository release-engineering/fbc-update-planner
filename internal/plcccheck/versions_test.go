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
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
)

func TestBundleMajorMinor(t *testing.T) {
	for _, version := range []string{
		"1.2", "1.2.0", "1.2.99", "1.2.0-rc.1", "1.2.0+build.01",
		"1.2.0-0.1.a-b+build.42", "1.2.0-01a", "1.2.999999999999999999999999999999",
	} {
		t.Run(version, func(t *testing.T) {
			got, err := bundleMajorMinor(version)
			if err != nil || got != (fbc.MajorMinor{Major: 1, Minor: 2}) {
				t.Errorf("got %v, %v; want 1.2", got, err)
			}
		})
	}
	for _, version := range []string{
		"", "1", "v1.2.3", "1.2garbage", "1.2-rc.1", "1.2+build", "1.2.3.4",
		"01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-rc.01", "1.2.3-", "1.2.3+",
		"1.2.3-rc..1", "1.2.3+build..1", "1.2.3-rc_1", " 1.2.3", "1.2.3\n", "-1.2.3",
		"18446744073709551616.2.0", "1.18446744073709551616.0",
	} {
		t.Run("invalid/"+version, func(t *testing.T) {
			if got, err := bundleMajorMinor(version); err == nil {
				t.Errorf("accepted %q as %s", version, got)
			}
		})
	}
	for _, version := range []string{"0.0.0", "0.0"} {
		if got, err := bundleMajorMinor(version); err != nil || got != (fbc.MajorMinor{}) {
			t.Errorf("zero version: %v, %v", got, err)
		}
	}
}
