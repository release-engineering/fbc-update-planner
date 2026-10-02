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

package catalog_test

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
)

const bundleJSON = `{"schema":"olm.bundle","package":"alpha","name":"alpha.v1.2.3","properties":[{"type":"olm.package","value":{"packageName":"alpha","version":"1.2.3"}}]}`

const lifecycleJSON = `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"alpha","versions":[{"name":"1.2"}]}`

func TestParseInventory(t *testing.T) {
	input := bundleJSON + `
{
  "schema": "olm.bundle",
  "package": "beta",
  "name": "beta.v2.0.1-rc.1",
  "properties": [
    {"type":"unused", "value":"` + strings.Repeat("x", 128*1024) + `"},
    {"type":"olm.package", "value":{"packageName":"beta", "version":"2.0.1-rc.1+build.7", "release":"2"}}
  ]
}
{"schema":"olm.package","name":"package-without-coverage-data"}
{"schema":"example.unknown","package":42,"properties":"unused"}
{"schema":"olm.channel","package":"alpha","entries":[]}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"alpha","versions":[
  {"name":"1.2", "phases":"unused", "platformCompatibility":42},
  {"name":"1.3"}
]}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"gamma","versions":[{"name":"9.1"}]}
{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"empty","versions":[]}
` + lifecycleJSON + bundleJSON
	got, err := catalog.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := &catalog.Inventory{Packages: map[string]catalog.Package{
		"alpha": {
			Bundles: []catalog.Bundle{
				{Name: "alpha.v1.2.3", Version: "1.2.3"},
				{Name: "alpha.v1.2.3", Version: "1.2.3"},
			},
			HasLifecycle:      true,
			LifecycleVersions: []string{"1.2", "1.3", "1.2"},
		},
		"beta":  {Bundles: []catalog.Bundle{{Name: "beta.v2.0.1-rc.1", Version: "2.0.1-rc.1+build.7"}}},
		"gamma": {HasLifecycle: true, LifecycleVersions: []string{"9.1"}},
		"empty": {HasLifecycle: true},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("inventory = %#v, want %#v", got.Packages, want.Packages)
	}
}

func TestParseEmptyInventory(t *testing.T) {
	for _, input := range []string{"", " \n\t", `{"schema":"olm.package","name":"alpha"}`} {
		got, err := catalog.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if got == nil || got.Packages == nil || len(got.Packages) != 0 {
			t.Errorf("Parse(%q) = %#v, want initialized empty inventory", input, got)
		}
	}
}

func TestParseLifecyclePresence(t *testing.T) {
	for _, versions := range []string{"", `,"versions":null`, `,"versions":[]`} {
		input := `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"alpha"` + versions + `}`
		got, err := catalog.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		pkg, found := got.Packages["alpha"]
		if !found || !pkg.HasLifecycle || len(pkg.LifecycleVersions) != 0 {
			t.Errorf("versions %q: package = %#v, found = %t", versions, pkg, found)
		}
	}
}

func TestParseIgnoresUnnamedLifecycle(t *testing.T) {
	for _, pkg := range []string{"", `,"package":null`, `,"package":""`} {
		// Unused contents must not be decoded after deciding to ignore the entry.
		input := `{"schema":"io.openshift.operators.lifecycles.v1alpha1","versions":42` + pkg + `}`
		got, err := catalog.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Packages) != 0 {
			t.Errorf("package %q: inventory = %#v, want empty", pkg, got.Packages)
		}
	}
}

func TestParsePreservesVersionText(t *testing.T) {
	input := strings.ReplaceAll(bundleJSON+lifecycleJSON, "1.2.3", "uninterpreted-bundle-version")
	input = strings.ReplaceAll(input, `"1.2"`, `"uninterpreted-lifecycle-version"`)
	got, err := catalog.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	pkg := got.Packages["alpha"]
	if pkg.Bundles[0].Version != "uninterpreted-bundle-version" || pkg.LifecycleVersions[0] != "uninterpreted-lifecycle-version" {
		t.Errorf("version text was changed: %#v", pkg)
	}
}

func TestParseRejectsMalformedRecords(t *testing.T) {
	withProperties := func(properties string) string {
		return fmt.Sprintf(`{"schema":"olm.bundle","package":"alpha","name":"alpha.v1","properties":%s}`, properties)
	}
	withValue := func(value string) string {
		return withProperties(`[{"type":"olm.package","value":` + value + `}]`)
	}
	withVersions := func(versions string) string {
		return `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"alpha","versions":` + versions + `}`
	}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"truncated JSON", `{"schema":`, "unexpected EOF"},
		{"trailing garbage", `oops`, "invalid character"},
		{"array envelope", `[]`, "cannot unmarshal array"},
		{"scalar envelope", `42`, "cannot unmarshal number"},
		{"null envelope", `null`, "schema"},
		{"missing schema", `{}`, "schema"},
		{"empty schema", `{"schema":""}`, "schema"},
		{"null schema", `{"schema":null}`, "schema"},
		{"numeric schema", `{"schema":42}`, "cannot unmarshal number"},
		{"missing bundle identity", `{"schema":"olm.bundle"}`, "package and name must be nonempty"},
		{"empty bundle name", strings.ReplaceAll(bundleJSON, `"alpha.v1.2.3"`, `""`), "package and name must be nonempty"},
		{"null bundle package", strings.ReplaceAll(bundleJSON, `"package":"alpha"`, `"package":null`), "package and name must be nonempty"},
		{"numeric bundle name", strings.ReplaceAll(bundleJSON, `"alpha.v1.2.3"`, `42`), "cannot unmarshal number"},
		{"missing properties", `{"schema":"olm.bundle","package":"alpha","name":"alpha.v1"}`, "found 0"},
		{"null properties", withProperties(`null`), "found 0"},
		{"wrong properties type", withProperties(`{}`), "cannot unmarshal object"},
		{"unrelated property", withProperties(`[{"type":"other","value":42}]`), "found 0"},
		{"duplicate package property", withProperties(`[{"type":"olm.package"},{"type":"olm.package"}]`), "found 2"},
		{"missing property value", withProperties(`[{"type":"olm.package"}]`), "olm.package property"},
		{"wrong property value type", withValue(`[]`), "olm.package property"},
		{"null property value", withValue(`null`), "does not match package"},
		{"missing property package", withValue(`{"version":"1.0.0"}`), "does not match package"},
		{"conflicting package", withValue(`{"packageName":"beta","version":"1.0.0"}`), "does not match package"},
		{"missing bundle version", withValue(`{"packageName":"alpha"}`), "version must be nonempty"},
		{"empty bundle version", withValue(`{"packageName":"alpha","version":""}`), "version must be nonempty"},
		{"null bundle version", withValue(`{"packageName":"alpha","version":null}`), "version must be nonempty"},
		{"numeric bundle version", withValue(`{"packageName":"alpha","version":42}`), "cannot unmarshal number"},
		{"numeric lifecycle package", `{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":42}`, "cannot unmarshal number"},
		{"wrong lifecycle versions type", withVersions(`{}`), "lifecycle versions"},
		{"wrong lifecycle version type", withVersions(`[42]`), "lifecycle versions"},
		{"null lifecycle version", withVersions(`[null]`), "name must be nonempty"},
		{"missing lifecycle name", withVersions(`[{}]`), "name must be nonempty"},
		{"empty lifecycle name", withVersions(`[{"name":""}]`), "name must be nonempty"},
		{"null lifecycle name", withVersions(`[{"name":null}]`), "name must be nonempty"},
		{"numeric lifecycle name", withVersions(`[{"name":42}]`), "cannot unmarshal number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A later error must discard records already read successfully.
			got, err := catalog.Parse(strings.NewReader(lifecycleJSON + "\n" + tt.input))
			if got != nil || err == nil {
				t.Fatalf("Parse() = (%#v, %v), want nil inventory and error", got, err)
			}
			for _, want := range []string{"catalog record 2", tt.want} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestParseErrorContext(t *testing.T) {
	input := strings.ReplaceAll(bundleJSON, `"version":"1.2.3"`, `"version":""`)
	_, err := catalog.Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"record 1", "olm.bundle", `package "alpha"`, `bundle "alpha.v1.2.3"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
	_, err = catalog.Parse(strings.NewReader(strings.ReplaceAll(lifecycleJSON, `"name":"1.2"`, `"name":""`)))
	if err == nil || !strings.Contains(err.Error(), `package "alpha"`) {
		t.Errorf("lifecycle error = %v, want package context", err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestParseReaderError(t *testing.T) {
	wantErr := errors.New("read failed")
	input := io.MultiReader(strings.NewReader(lifecycleJSON), errorReader{err: wantErr})
	got, err := catalog.Parse(input)
	if got != nil || !errors.Is(err, wantErr) {
		t.Errorf("Parse() = (%#v, %v), want nil inventory and wrapped read error", got, err)
	}
}
