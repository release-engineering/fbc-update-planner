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
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

const comparisonLifecycleJSON = `{
  "schema": "io.openshift.operators.lifecycles.v1alpha1",
  "package": "operator",
  "versions": [
    {"name":"1.2", "phases":[
      {"name":"Full support", "startDate":"2020-01-01", "endDate":"2020-06-30"},
      {"name":"Maintenance support", "startDate":"2020-07-01", "endDate":"2020-12-31"}
    ], "platformCompatibility":[
      {"name":"openshift", "versions":["4.18","4.19"]},
      {"name":"another-platform", "versions":["2.0"]}
    ]},
    {"name":"2.0", "phases":[
      {"name":"Full support", "startDate":"2021-01-01", "endDate":"2021-12-31"}
    ]}
  ]
}`

func TestLifecycleMatches(t *testing.T) {
	var generated fbc.Package
	if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &generated); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(map[string]any)
		want bool
	}{
		{name: "same contents with different formatting", want: true},
		{name: "unordered lists", want: true, edit: func(record map[string]any) {
			versions := record["versions"].([]any)
			platforms := versions[0].(map[string]any)["platformCompatibility"].([]any)
			slices.Reverse(platforms[0].(map[string]any)["versions"].([]any))
			slices.Reverse(platforms)
			slices.Reverse(versions)
		}},
		{name: "extra root field", edit: func(record map[string]any) { record["extra"] = "value" }},
		{name: "missing version without a bundle", edit: func(record map[string]any) {
			record["versions"] = record["versions"].([]any)[:1]
		}},
		{name: "extra version without a bundle", edit: func(record map[string]any) {
			versions := record["versions"].([]any)
			record["versions"] = append(versions, map[string]any{"name": "3.0"})
		}},
		{name: "duplicate version", edit: func(record map[string]any) {
			versions := record["versions"].([]any)
			record["versions"] = append(versions, versions[0])
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &record); err != nil {
				t.Fatal(err)
			}
			if tt.edit != nil {
				tt.edit(record)
			}
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			assertLifecycleMatch(t, []*fbc.Package{&generated}, []json.RawMessage{raw}, tt.want)
		})
	}
	for _, tt := range []struct{ name, old, replacement string }{
		{"phase name", "Maintenance support", "Extended support"},
		{"start date", "2020-07-01", "2020-07-02"},
		{"end date", "2020-12-31", "2020-12-30"},
		{"platform name", "openshift", "other"},
		{"platform version", "4.18", "4.17"},
		{"extra version field", `"name":"1.2"`, `"name":"1.2","extra":true`},
		{"extra phase field", `"name":"Full support"`, `"name":"Full support","extra":true`},
		{"extra platform field", `"name":"openshift"`, `"name":"openshift","extra":true`},
		{"missing date", `"startDate":"2020-01-01",`, ""},
		{"null date", `"startDate":"2020-01-01"`, `"startDate":null`},
		{"duplicate compatible version", `["4.18","4.19"]`, `["4.18","4.19","4.18"]`},
		{"wrong phases type", `"phases":[`, `"phases":null,"unused":[`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(strings.Replace(comparisonLifecycleJSON, tt.old, tt.replacement, 1))
			assertLifecycleMatch(t, []*fbc.Package{&generated}, []json.RawMessage{raw}, false)
		})
	}
	t.Run("phase ordering", func(t *testing.T) {
		var shipped fbc.Package
		if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &shipped); err != nil {
			t.Fatal(err)
		}
		slices.Reverse(shipped.Versions[0].Phases)
		raw, err := json.Marshal(shipped)
		if err != nil {
			t.Fatal(err)
		}
		assertLifecycleMatch(t, []*fbc.Package{&generated}, []json.RawMessage{raw}, false)
	})
	t.Run("omitted optional compatibility differs from null and empty", func(t *testing.T) {
		for _, value := range []string{"null", "[]"} {
			raw := json.RawMessage(strings.Replace(comparisonLifecycleJSON, `"name":"2.0"`, `"name":"2.0","platformCompatibility":`+value, 1))
			assertLifecycleMatch(t, []*fbc.Package{&generated}, []json.RawMessage{raw}, false)
		}
	})
	t.Run("record multiplicity", func(t *testing.T) {
		raw := json.RawMessage(comparisonLifecycleJSON)
		assertLifecycleMatch(t, []*fbc.Package{&generated}, []json.RawMessage{raw, raw}, false)
		assertLifecycleMatch(t, []*fbc.Package{&generated, &generated}, []json.RawMessage{raw, raw}, true)
	})
}

func assertLifecycleMatch(t *testing.T, generated []*fbc.Package, shipped []json.RawMessage, want bool) {
	t.Helper()
	beforeGenerated, err := json.Marshal(generated)
	if err != nil {
		t.Fatal(err)
	}
	beforeShipped, err := json.Marshal(shipped)
	if err != nil {
		t.Fatal(err)
	}
	var previous []LifecycleDifference
	for attempt := range 2 {
		got, err := lifecycleDifferences(generated, shipped)
		if err != nil || (len(got) == 0) != want {
			t.Fatalf("lifecycleDifferences() = %+v, %v; want match %v", got, err, want)
		}
		if attempt > 0 && !reflect.DeepEqual(got, previous) {
			t.Fatalf("non-deterministic lifecycle differences: %+v, previously %+v", got, previous)
		}
		previous = got
	}
	afterGenerated, err := json.Marshal(generated)
	if err != nil {
		t.Fatal(err)
	}
	afterShipped, err := json.Marshal(shipped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeGenerated, afterGenerated) || !bytes.Equal(beforeShipped, afterShipped) {
		t.Fatal("comparison mutated its inputs")
	}
}

func TestLifecycleDifferenceDetails(t *testing.T) {
	var generated fbc.Package
	if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &generated); err != nil {
		t.Fatal(err)
	}
	const versionPath = `lifecycles[0].versions[name="1.2"]`
	const phasePath = versionPath + `.phases[0]`
	for _, tt := range []struct {
		name, old, replacement, path string
		plcc, catalog                json.RawMessage
	}{
		{"date changed", "2020-01-01", "2020-01-02", phasePath + ".startDate", json.RawMessage(`"2020-01-01"`), json.RawMessage(`"2020-01-02"`)},
		{"phase renamed", "Maintenance support", "Extended support", versionPath + ".phases[1].name", json.RawMessage(`"Maintenance support"`), json.RawMessage(`"Extended support"`)},
		{"missing date", `"startDate":"2020-01-01",`, "", phasePath + ".startDate", json.RawMessage(`"2020-01-01"`), nil},
		{"null date", `"startDate":"2020-01-01"`, `"startDate":null`, phasePath + ".startDate", json.RawMessage(`"2020-01-01"`), json.RawMessage(`null`)},
		{"unknown field", `"name":"1.2"`, `"name":"1.2","extra":true`, versionPath + ".extra", nil, json.RawMessage(`true`)},
		{"escaped unknown key", `"name":"1.2"`, `"name":"1.2","a.b[0]":null`, versionPath + `["a.b[0]"]`, nil, json.RawMessage(`null`)},
		{"unknown ordered list", `"name":"1.2"`, `"name":"1.2","extra":[2,1]`, versionPath + ".extra", nil, json.RawMessage(`[2,1]`)},
		{"platform version", "4.18", "4.17", versionPath + `.platformCompatibility[name="openshift"].versions`, json.RawMessage(`["4.18","4.19"]`), json.RawMessage(`["4.17","4.19"]`)},
		{"duplicate platform version", `["4.18","4.19"]`, `["4.18","4.19","4.18"]`, versionPath + `.platformCompatibility[name="openshift"].versions`, json.RawMessage(`["4.18","4.19"]`), json.RawMessage(`["4.18","4.18","4.19"]`)},
		{"omitted versus null", `"name":"2.0"`, `"name":"2.0","platformCompatibility":null`, `lifecycles[0].versions[name="2.0"].platformCompatibility`, nil, json.RawMessage(`null`)},
		{"omitted versus empty", `"name":"2.0"`, `"name":"2.0","platformCompatibility":[]`, `lifecycles[0].versions[name="2.0"].platformCompatibility`, nil, json.RawMessage(`[]`)},
		{"malformed phases", `"phases":[
      {"name":"Full support", "startDate":"2021-01-01", "endDate":"2021-12-31"}
    ]`, `"phases":false`, `lifecycles[0].versions[name="2.0"].phases`, json.RawMessage(`[{"endDate":"2021-12-31","name":"Full support","startDate":"2021-01-01"}]`), json.RawMessage(`false`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(strings.Replace(comparisonLifecycleJSON, tt.old, tt.replacement, 1))
			got, err := lifecycleDifferences([]*fbc.Package{&generated}, []json.RawMessage{raw})
			want := []LifecycleDifference{{Path: tt.path, PLCC: tt.plcc, Catalog: tt.catalog}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("differences = %+v, %v; want %+v", got, err, want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var decoded []LifecycleDifference
			if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, want) {
				t.Fatalf("JSON lost absent/null/value evidence: %s, %v", encoded, err)
			}
		})
	}
}

func TestLifecycleDifferenceListMatching(t *testing.T) {
	var generated fbc.Package
	if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &generated); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path string
		edit       func(map[string]any)
		absentPLCC bool
	}{
		{"missing version leaves other versions matched", `lifecycles[0].versions[name="1.2"]`, func(record map[string]any) {
			record["versions"] = record["versions"].([]any)[1:]
		}, false},
		{"extra version leaves other versions matched", `lifecycles[0].versions[name="0.1"]`, func(record map[string]any) {
			record["versions"] = append(record["versions"].([]any), map[string]any{"name": "0.1"})
		}, true},
		{"duplicate version", `lifecycles[0].versions[name="1.2"][1]`, func(record map[string]any) {
			versions := record["versions"].([]any)
			record["versions"] = append(versions, versions[0])
		}, true},
		{"missing platform", `lifecycles[0].versions[name="1.2"].platformCompatibility[name="openshift"]`, func(record map[string]any) {
			version := record["versions"].([]any)[0].(map[string]any)
			version["platformCompatibility"] = version["platformCompatibility"].([]any)[1:]
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal([]byte(comparisonLifecycleJSON), &record); err != nil {
				t.Fatal(err)
			}
			tt.edit(record)
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			differences, err := lifecycleDifferences([]*fbc.Package{&generated}, []json.RawMessage{raw})
			if err != nil || len(differences) != 1 || differences[0].Path != tt.path ||
				(differences[0].PLCC == nil) != tt.absentPLCC || (differences[0].Catalog == nil) == tt.absentPLCC {
				t.Fatalf("unexpected list differences: %+v, %v", differences, err)
			}
		})
	}
	t.Run("match unchanged records before pairing changed records", func(t *testing.T) {
		changed := json.RawMessage(strings.Replace(comparisonLifecycleJSON, "2020-01-01", "2020-01-02", 1))
		unchanged := json.RawMessage(comparisonLifecycleJSON)
		differences, err := lifecycleDifferences([]*fbc.Package{&generated, &generated}, []json.RawMessage{changed, unchanged})
		if err != nil || len(differences) != 1 || differences[0].Path != `lifecycles[1].versions[name="1.2"].phases[0].startDate` {
			t.Fatalf("unchanged record was not matched: %+v, %v", differences, err)
		}
	})
}

func TestAssessLifecycleComparison(t *testing.T) {
	matched, mismatched := true, false
	for _, tt := range []struct {
		name   string
		edit   func(*plcc.Catalog, *catalog.Inventory)
		status CatalogStatus
		plcc   PLCCStatus
		action Action
		match  *bool
	}{
		{name: "match", status: CatalogOK, plcc: PLCCOK, action: OK, match: &matched},
		{name: "date changed", status: CatalogMismatch, plcc: PLCCOK, action: BuildOperator, match: &mismatched, edit: func(_ *plcc.Catalog, inventory *catalog.Inventory) {
			pkg := inventory.Packages["full"]
			pkg.LifecycleRecords[0] = bytes.Replace(pkg.LifecycleRecords[0], []byte("2020-12-31"), []byte("2020-12-30"), 1)
		}},
		{name: "PLCC version removed", status: CatalogMismatch, plcc: PLCCRegressed, action: FixPLCC, match: &mismatched, edit: func(source *plcc.Catalog, _ *catalog.Inventory) {
			source.FindProductByName("full").Versions = source.FindProductByName("full").Versions[:1]
		}},
		{name: "no bundles takes precedence", status: CatalogNoBundles, plcc: PLCCOK, action: AddOperator, match: &mismatched, edit: func(_ *plcc.Catalog, inventory *catalog.Inventory) {
			pkg := inventory.Packages["full"]
			pkg.Bundles = nil
			pkg.LifecycleRecords = append(pkg.LifecycleRecords, pkg.LifecycleRecords[0])
			inventory.Packages["full"] = pkg
		}},
		{name: "incomplete takes precedence", status: CatalogIncomplete, plcc: PLCCOK, action: BuildOperator, match: &mismatched, edit: func(_ *plcc.Catalog, inventory *catalog.Inventory) {
			pkg := inventory.Packages["full"]
			pkg.LifecycleVersions = []string{"1.2"}
			pkg.LifecycleRecords = []json.RawMessage{json.RawMessage(`{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"full","versions":[{"name":"1.2"}]}`)}
			inventory.Packages["full"] = pkg
		}},
		{name: "source missing", status: CatalogOK, plcc: PLCCAbsent, action: AddPLCC, edit: func(source *plcc.Catalog, _ *catalog.Inventory) {
			source.FindProductByName("full").Package = "other"
		}},
		{name: "translation rejected", status: CatalogOK, plcc: PLCCInvalid, action: FixPLCC, edit: func(source *plcc.Catalog, _ *catalog.Inventory) {
			source.FindProductByName("full").Versions[0].Phases = nil
		}},
		{name: "metadata-only inventory", status: CatalogOK, plcc: PLCCOK, action: OK, edit: func(_ *plcc.Catalog, inventory *catalog.Inventory) {
			pkg := inventory.Packages["full"]
			pkg.LifecycleRecords = nil
			inventory.Packages["full"] = pkg
		}},
		{name: "lifecycle missing", status: CatalogMissing, plcc: PLCCOK, action: BuildOperator, edit: func(_ *plcc.Catalog, inventory *catalog.Inventory) {
			pkg := inventory.Packages["full"]
			pkg.HasLifecycle, pkg.LifecycleVersions, pkg.LifecycleRecords = false, nil, nil
			inventory.Packages["full"] = pkg
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, inventory := fixtures(t)
			if tt.edit != nil {
				tt.edit(source, inventory)
			}
			a := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"full"}, Validators: []string{"none"}})
			pkg := a.Packages[0]
			if pkg.Catalog.Status() != tt.status || pkg.PLCC != tt.plcc || pkg.Action != tt.action || !reflect.DeepEqual(pkg.Catalog.MatchesPLCC, tt.match) {
				t.Fatalf("unexpected assessment: %+v, catalog %+v", pkg, pkg.Catalog)
			}
			mismatch := tt.match != nil && !*tt.match
			if pkg.hasIssue(MismatchedCatalogLifecycle) != mismatch {
				t.Fatalf("unexpected mismatch evidence: %+v", pkg.Issues)
			}
			if (len(pkg.Catalog.LifecycleDifferences) > 0) != mismatch {
				t.Fatalf("unexpected difference evidence: %+v", pkg.Catalog.LifecycleDifferences)
			}
			data, err := json.Marshal(pkg.Catalog)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(`"matchesPLCC"`)) != (tt.match != nil) {
				t.Fatalf("comparison availability lost in JSON: %s", data)
			}
			if bytes.Contains(data, []byte(`"lifecycleDifferences"`)) != mismatch {
				t.Fatalf("difference evidence lost in JSON: %s", data)
			}
		})
	}
}

func TestAssessLifecycleAliasesAndMultipleProducts(t *testing.T) {
	source, _ := fixtures(t)
	options := plcc.DatasetOptions{Packages: []string{"alpha", "beta"}, Validators: []string{"none"}}
	baseline := mustAssess(t, source, nil, options)
	var stream bytes.Buffer
	for _, bundle := range []string{
		`{"schema":"olm.bundle","package":"alpha","name":"alpha.v1.2.0","properties":[{"type":"olm.package","value":{"packageName":"alpha","version":"1.2.0"}}]}`,
		`{"schema":"olm.bundle","package":"beta","name":"beta.v1.2.0","properties":[{"type":"olm.package","value":{"packageName":"beta","version":"1.2.0"}}]}`,
		`{"schema":"olm.bundle","package":"beta","name":"beta.v1.3.0","properties":[{"type":"olm.package","value":{"packageName":"beta","version":"1.3.0"}}]}`,
	} {
		stream.WriteString(bundle)
	}
	encoder := json.NewEncoder(&stream)
	// Catalog record order can differ from the generated artifact's order.
	for i := len(baseline.FBC) - 1; i >= 0; i-- {
		if err := encoder.Encode(baseline.FBC[i]); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := catalog.Parse(&stream)
	if err != nil {
		t.Fatal(err)
	}
	assessment := mustAssess(t, source, inventory, options)
	for _, pkg := range assessment.Packages {
		if pkg.Action != OK || pkg.Catalog.MatchesPLCC == nil || !*pkg.Catalog.MatchesPLCC {
			t.Fatalf("multiple products or aliases did not match: %+v", pkg)
		}
	}
	pkg := inventory.Packages["beta"]
	pkg.LifecycleRecords = append(pkg.LifecycleRecords, pkg.LifecycleRecords[0])
	inventory.Packages["beta"] = pkg
	assessment = mustAssess(t, source, inventory, options)
	if beta := findPackage(t, assessment, "beta"); beta.Action != BuildOperator || beta.Catalog.Status() != CatalogMismatch {
		t.Fatalf("extra catalog record was ignored: %+v", beta)
	}
	// One successful translation must not establish equality for a package
	// whose other source product was rejected.
	source.FindProductByName("second beta").Versions[0].Phases = nil
	assessment = mustAssess(t, source, inventory, options)
	if beta := findPackage(t, assessment, "beta"); beta.Action != FixPLCC || beta.Catalog.MatchesPLCC != nil || beta.hasIssue(MismatchedCatalogLifecycle) {
		t.Fatalf("compared a partially translated package: %+v", beta)
	}
	if alpha := findPackage(t, assessment, "alpha"); alpha.Action != OK || alpha.Catalog.MatchesPLCC == nil || !*alpha.Catalog.MatchesPLCC {
		t.Fatalf("beta failure affected alpha comparison: %+v", alpha)
	}
}

func TestAssessLifecycleComparisonErrorsAreScoped(t *testing.T) {
	source, inventory := fixtures(t)
	pkg := inventory.Packages["full"]
	pkg.LifecycleRecords = []json.RawMessage{json.RawMessage(`{"versions":`)}
	inventory.Packages["full"] = pkg
	result, err := Assess(source, inventory, plcc.DatasetOptions{Packages: []string{"full"}, Validators: []string{"none"}})
	if result != nil || err == nil || !strings.Contains(err.Error(), `package "full" lifecycle comparison`) {
		t.Fatalf("malformed raw lifecycle = %v, %v; want contextual error", result, err)
	}
	mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"unpublished"}, Validators: []string{"none"}})
}
