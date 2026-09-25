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
	"strings"
	"testing"
)

func TestParseRenderCatalogScopeAndPatchCollapse(t *testing.T) {
	const stream = `
{"schema":"olm.package","name":"ignored"}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"lifecycle-only","versions":[]}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"covered","versions":[{"name":"1.2"}]}
{"schema":"olm.bundle","package":"covered","properties":[{"type":"olm.package","value":{"version":"1.2.3"}}]}
{"schema":"olm.bundle","package":"covered","properties":[{"type":"olm.package","value":{"version":"1.2.4"}}]}
{"schema":"olm.bundle","package":"bundle-only","properties":[{"type":"olm.package","value":{"version":"2.1.0-rc.1+build.5"}}]}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":null,"versions":[{"name":"9.9"}]}
`
	got, err := ParseRender(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if !got.LifecyclePackages["lifecycle-only"] || len(got.LifecycleVersions["lifecycle-only"]) != 0 {
		t.Errorf("empty lifecycle entry was lost: %+v", got)
	}
	if !got.LifecycleVersions["covered"]["1.2"] || len(got.BundleVersions["covered"]) != 1 || !got.BundleVersions["covered"]["1.2"] {
		t.Errorf("patch bundles did not collapse to one MAJOR.MINOR: %+v", got)
	}
	if got.LifecyclePackages["bundle-only"] || !got.BundleVersions["bundle-only"]["2.1"] {
		t.Errorf("bundle-only package was lost: %+v", got)
	}
	if got.LifecyclePackages["null"] || got.LifecyclePackages[""] {
		t.Errorf("null package became a catalog package: %+v", got)
	}
}

func TestParseRenderRejectsUntrustworthyCatalog(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
	}{
		{"malformed JSON", `{"schema":"olm.bundle",`},
		{"bad lifecycle version", `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"a","versions":[{"name":"1.2.3"}]}`},
		{"bad bundle version", `{"schema":"olm.bundle","package":"a","properties":[{"type":"olm.package","value":{"version":"latest"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRender(strings.NewReader(tc.stream)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
