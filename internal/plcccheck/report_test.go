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
	"encoding/csv"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestReportStatusesCountsAndDetails(t *testing.T) {
	source, inventory := fixtures(t)
	source.FindProductByName("full").Versions = source.FindProductByName("full").Versions[:1]
	names := []string{"full", "mixed", "unpublished", "bundle-only", "beta", "validator", "no-bundles"}
	assessment := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: names, Validators: []string{"syntax", "catalog"}})
	r := NewReport(assessment)
	assertSummaryPresentation(t, r, "READY operators: 0/7")
	want := Summary{
		Total:      7,
		NonSkipped: 7,
		PLCC:       PLCCCounts{OK: 2, Missing: 1, Duplicate: 1, Invalid: 1, Regressed: 1, Incomplete: 1},
		Catalog:    &CatalogCounts{OK: 1, Incomplete: 1, Missing: 4, NoBundles: 1},
	}
	if !reflect.DeepEqual(r.Summary, want) {
		t.Fatalf("summary = %+v, want %+v", r.Summary, want)
	}
	text := r.Text()
	if strings.Contains(r.summaryText(), "Catalog X/Y") || !strings.Contains(text, "\n\n"+tableCoverageLegend+"\n   ACTION") {
		t.Fatalf("coverage legend must accompany the table, outside the summary: %s", text)
	}
	for _, want := range []string{
		"PLCC: OK 2 | MISSING 1 | INCOMPLETE 1 | INVALID 1 | REGRESSED 1 | DUPLICATE 1\n",
		"Catalog: OK 1 | MISSING 4 | INCOMPLETE 1 | NO BUNDLES 1\n",
		"ACTION", "OPERATOR", "PLCC", "CATALOG",
		"REGRESSED", "INCOMPLETE", "NO BUNDLES", "1/3",
		"PLCC version 1.10 is missing but its lifecycle data is already shipped",
		"PLCC version 1.4 is missing; required by catalog bundles.",
		"Catalog lifecycle version 1.3 is missing; required by catalog bundles.",
		"REQ-VAL-01",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}
	_, details, ok := strings.Cut(text, "\nDetails\n")
	if !ok {
		t.Fatal("no details section")
	}
	previous := -1
	for _, pkg := range assessment.Packages {
		at := strings.Index(details, "\n"+pkg.Name+"\n")
		if at <= previous {
			t.Errorf("details not grouped in requested package order: %q", details)
		}
		previous = at
		for _, failure := range pkg.Failures {
			for _, reason := range failure.Reasons {
				if !strings.Contains(details, reason) {
					t.Errorf("lost reason %q", reason)
				}
			}
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Summary struct {
			Catalog map[string]int `json:"catalog"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if got := wire.Summary.Catalog; !reflect.DeepEqual(got, map[string]int{"ok": 1, "missing": 4, "incomplete": 1, "noBundles": 1}) {
		t.Fatalf("unexpected catalog summary JSON fields: %v", got)
	}
	var decoded Report
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Summary, want) || decoded.Assessment.Packages[0].Issues[0].Kind != RegressedPLCCVersion {
		t.Fatalf("JSON lost summary or issue evidence: %s", encoded)
	}
	if strings.Contains(string(encoded), "filteredPLCC") || strings.Contains(string(encoded), "phases") {
		t.Fatal("assessment JSON contains pipeline artifacts")
	}
}

func TestReportCatalogSource(t *testing.T) {
	for _, tt := range []struct{ name, image, input, label string }{
		{"image", "registry.example.test/catalog:v5.0", "", "Catalog image: registry.example.test/catalog:v5.0"},
		{"rendered input", "", "catalog.json", "Catalog input: catalog.json"},
		{"literal source", "registry.example.test/<!channel>\nindex", "", "Catalog image: registry.example.test/<!channel>\\nindex"},
		{"no catalog", "", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReport(&Assessment{})
			r.CatalogImage, r.CatalogInput = tt.image, tt.input
			payload, err := r.Slack(SlackOptions{Summary: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
			if err != nil {
				t.Fatal(err)
			}
			var slack strings.Builder
			for _, block := range payload.Blocks {
				slack.WriteString(slackBlockText(t, block))
			}
			for format, value := range map[string]string{"text": r.Text(), "Slack": slack.String()} {
				if tt.label != "" && !strings.Contains(value, tt.label+"\n\nTotal operators:") {
					t.Errorf("%s missing source or blank line after %q: %s", format, tt.label, value)
				}
				if strings.Contains(value, "Catalog image:") != (tt.image != "") || strings.Contains(value, "Catalog input:") != (tt.input != "") {
					t.Errorf("%s mislabels the catalog source: %s", format, value)
				}
			}
			assertSummaryPresentation(t, r, "")
		})
	}
}

// Check the shared order and Slack block types as well as the rendered text.
func assertSummaryPresentation(t *testing.T, r *Report, ready string) {
	t.Helper()
	payload, err := r.Slack(SlackOptions{Summary: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	var summary strings.Builder
	foundReady := false
	// The outer title and artifact footer are not part of the Summary section.
	for i := 1; i < len(payload.Blocks)-1; i++ {
		block := payload.Blocks[i]
		value := slackBlockText(t, block)
		if strings.HasPrefix(value, "READY operators:") {
			if ready == "" || value != ready || block.Type != "header" || block.Text.Type != "plain_text" {
				t.Fatalf("unexpected readiness heading: %+v; want %q", block, ready)
			}
			if !strings.HasSuffix(slackBlockText(t, payload.Blocks[i-1]), "(Status counts exclude skipped operators)") ||
				!strings.HasPrefix(slackBlockText(t, payload.Blocks[i+1]), "PLCC:") {
				t.Fatal("READY must appear between skipped operators and status counts")
			}
			foundReady = true
		}
		summary.WriteString(value)
		summary.WriteByte('\n')
	}
	text := r.summaryText()
	if summary.String() != text || foundReady != (ready != "") {
		t.Fatalf("summary mismatch: Slack %q, text %q, want ready %q", summary.String(), text, ready)
	}
	if strings.Contains(text, "Non-skipped operators:") || strings.Contains(text, "Fully OK") {
		t.Fatalf("obsolete summary labels: %s", text)
	}
	_, skippedAndRest, ok := strings.Cut(text, "\nSkipped operators: ")
	skipped, _, _ := strings.Cut(skippedAndRest, "\n")
	if !ok || !strings.Contains(skipped, "    (Status counts exclude skipped operators)") {
		t.Fatalf("missing inline skipped explanation: %s", text)
	}
}

// Read the CSV lists from the complete text artifact independently of Slack.
func textListGroups(t *testing.T, report string) map[string][]string {
	t.Helper()
	_, lists, ok := strings.Cut(report, "\nList\n")
	if !ok {
		t.Fatal("missing text lists")
	}
	lists, _, ok = strings.Cut(lists, "\nDetails\n")
	if !ok {
		t.Fatal("missing details after text lists")
	}
	groups := make(map[string][]string)
	for _, block := range strings.Split(strings.TrimSpace(lists), "\n\n") {
		heading, record, ok := strings.Cut(block, "\n")
		if !ok {
			t.Fatalf("missing CSV for %q", heading)
		}
		group, _, ok := strings.Cut(heading, " - ")
		if !ok {
			t.Fatalf("missing description for %q", heading)
		}
		icon, group, ok := strings.Cut(group, " ")
		wantIcon := map[string]string{"OK": "✅", "PLCCDATA": "📋", "OPERATOR": "📦", "SKIPPED": "➖"}[group]
		if !ok || wantIcon == "" || icon != wantIcon {
			t.Fatalf("incorrect icon in list heading %q", heading)
		}
		if record == "None" {
			groups[group] = nil
			continue
		}
		rows, err := csv.NewReader(strings.NewReader(record)).ReadAll()
		if err != nil || len(rows) != 1 {
			t.Fatalf("invalid text CSV %q: %v", record, err)
		}
		groups[group] = rows[0]
	}
	return groups
}

func TestReportPLCCVersionCoverage(t *testing.T) {
	for _, tt := range []struct {
		name            string
		source, shipped []string
		status          PLCCStatus
		plcc, catalog   string
	}{
		{"partial with extra source version", []string{"1.2", "1.3", "9.0"}, []string{"1.2"}, PLCCIncomplete, "2/3", "1/3"},
		{"no required versions available", []string{"9.0"}, nil, PLCCIncomplete, "0/3", "0/3"},
		{"regression takes precedence", []string{"1.3", "9.0"}, []string{"1.2"}, PLCCRegressed, "REGRESSED", "1/3"},
		{"all required versions available", []string{"1.2", "1.3", "1.4", "9.0"}, []string{"1.2"}, PLCCOK, "OK", "1/3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, inventory := fixtures(t)
			product := source.FindProductByName("mixed")
			template := product.Versions[0]
			product.Versions = nil
			for _, name := range tt.source {
				version := template
				version.Name = name
				product.Versions = append(product.Versions, version)
			}
			pkg := inventory.Packages["mixed"]
			pkg.LifecycleVersions = tt.shipped
			// Multiple patches and duplicate bundles still require one entry
			// for MAJOR.MINOR 1.2 in both sources.
			pkg.Bundles = append(pkg.Bundles, pkg.Bundles[0], catalog.Bundle{Name: "mixed.v1.2.99", Version: "1.2.99"})
			inventory.Packages["mixed"] = pkg
			r := NewReport(mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"mixed"}, Validators: []string{"syntax"}}))
			if got := strings.Join(strings.Fields(r.Text()), " "); !strings.Contains(got, "mixed "+tt.plcc+" "+tt.catalog) {
				t.Fatalf("incorrect text table coverage: %s", got)
			}
			payload, err := r.Slack(SlackOptions{Table: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
			if err != nil {
				t.Fatal(err)
			}
			var slack strings.Builder
			for _, block := range payload.Blocks {
				slack.WriteString(slackBlockText(t, block))
			}
			slackPLCC := tt.plcc
			if slackPLCC == "OK" {
				slackPLCC = "✅"
			}
			if got := strings.Join(strings.Fields(slack.String()), " "); !strings.Contains(got, "mixed | "+slackPLCC+" | "+tt.catalog) {
				t.Fatalf("incorrect Slack table coverage: %s", got)
			}
			encoded, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), `"plcc":"`+string(tt.status)+`"`) || (r.Summary.PLCC.Incomplete == 1) != (tt.status == PLCCIncomplete) {
				t.Fatalf("table formatting changed JSON classification or summary counts: %s", encoded)
			}
		})
	}
}

func TestReportCatalogLifecycleDetails(t *testing.T) {
	source, inventory := fixtures(t)
	for _, tt := range []struct {
		name                             string
		entryMissing, plccVersionMissing bool
	}{
		{"converter", true, true},
		{"unpublished", true, false},
		{"empty-lifecycle", false, false},
		{"mixed", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReport(mustAssess(t, source, inventory, plcc.DatasetOptions{
				Packages: []string{tt.name}, Validators: []string{"syntax"},
			}))
			payload, err := r.Slack(SlackOptions{Details: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
			if err != nil {
				t.Fatal(err)
			}
			var slack strings.Builder
			for _, block := range payload.Blocks {
				slack.WriteString(slackBlockText(t, block))
			}
			for format, details := range map[string]string{"text": r.Text(), "Slack": slack.String()} {
				if strings.Contains(details, "[catalog-lifecycle-missing]") != tt.entryMissing ||
					strings.Contains(details, "[catalog-lifecycle-version-missing]") == tt.entryMissing {
					t.Errorf("%s catalog findings are redundant or missing: %s", format, details)
				}
				if strings.Contains(details, "[plcc-version-missing]") != tt.plccVersionMissing {
					t.Errorf("%s lost a PLCC version finding: %s", format, details)
				}
			}
			if !r.Assessment.Packages[0].hasIssue(MissingCatalogLifecycleVersion) {
				t.Fatal("rendering removed version evidence from the assessment")
			}
		})
	}
}

func TestReportUncheckedAndEmptyCatalog(t *testing.T) {
	source, inventory := fixtures(t)
	for _, test := range []struct {
		name      string
		inventory *catalog.Inventory
		names     []string
		checked   bool
		fullyOK   int
		ready     string
	}{
		{"unchecked", nil, []string{"full"}, false, 0, ""},
		{"checked", inventory, []string{"full"}, true, 1, "READY operators: 1/1"},
		{"empty catalog", &catalog.Inventory{}, []string{"full"}, true, 0, "READY operators: 0/1"},
		{"empty selection", inventory, []string{}, true, 0, "READY operators: 0/0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := NewReport(mustAssess(t, source, test.inventory, plcc.DatasetOptions{Packages: test.names, Validators: []string{"syntax"}}))
			assertSummaryPresentation(t, r, test.ready)
			if (r.Summary.Catalog != nil) != test.checked || r.Summary.FullyOK != test.fullyOK {
				t.Fatalf("incorrect catalog summary: %+v", r.Summary)
			}
			if !test.checked && !strings.Contains(r.Text(), "PLCC OK; catalog not checked") {
				t.Fatal("unchecked catalog presented as fully ready")
			}
		})
	}
}

func TestAssessmentRetainsPipelineOutputs(t *testing.T) {
	source, inventory := fixtures(t)
	before, _ := json.Marshal(source)
	assessment := mustAssess(t, source, inventory, plcc.DatasetOptions{
		Packages: []string{"beta", "full", "converter", "filter"}, Validators: []string{"none"},
	})
	// Both duplicate beta products are retained when duplicate checking is
	// disabled. Conversion and filter failures stay in the PLCC dump only.
	var produced []string
	for _, pkg := range assessment.FBC {
		produced = append(produced, pkg.Name)
	}
	if !reflect.DeepEqual(produced, []string{"beta", "beta", "full"}) || len(assessment.FilteredPLCC.Data) != 5 {
		t.Fatalf("pipeline outputs lost products: FBC %v, PLCC %+v", produced, assessment.FilteredPLCC)
	}
	assessment.FilteredPLCC.Data[0].Versions[0].Name = "99.0"
	assessment.FBC[0].Name = "changed"
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("retained pipeline output aliases the source")
	}
	if findPackage(t, assessment, "beta").ProducedVersions[0].String() != "1.2" {
		t.Fatal("pipeline output mutation changed report evidence")
	}
}

func TestReportKeepsUntrustedTextOnOneLine(t *testing.T) {
	r := NewReport(&Assessment{Packages: []PackageAssessment{{
		Name: "operator\nFAKE\tROW", PLCC: PLCCInvalid, Action: FixPLCC,
		Failures: []Failure{{Stage: FBCTranslation, Reasons: []string{"reason\nnext\x1b"}}},
	}}})
	text := r.Text()
	if strings.Contains(text, "operator\nFAKE") || !strings.Contains(text, "operator\\nFAKE\\tROW") || !strings.Contains(text, "reason\\nnext\\x1b") {
		t.Fatalf("unexpected escaping: %q", text)
	}
}

func TestSkippedReports(t *testing.T) {
	for _, checked := range []bool{false, true} {
		for _, allSkipped := range []bool{false, true} {
			source, inventory := fixtures(t)
			if !checked {
				inventory = nil
			}
			names := []string{"full", "mixed", "validator", "bundle-only", "stale", "unpublished", "no-bundles"}
			a := mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: names, Validators: []string{"syntax"}})
			reasons := map[string]string{"mixed": "Group note", "validator": "Group note", "bundle-only": "Individual note", "stale": "Individual note"}
			if allSkipped {
				for _, name := range names {
					reasons[name] = "All skipped"
				}
			}
			if err := a.ApplySkips(reasons); err != nil {
				t.Fatal(err)
			}
			r := NewReport(a)
			want := Summary{Total: 7, NonSkipped: 3, Skipped: 4, PLCC: PLCCCounts{OK: 3}}
			if checked {
				want.FullyOK = 1
				want.Catalog = &CatalogCounts{OK: 1, Missing: 1, NoBundles: 1}
			}
			if allSkipped {
				want.NonSkipped, want.Skipped, want.FullyOK, want.PLCC = 0, 7, 0, PLCCCounts{}
				if checked {
					want.Catalog = &CatalogCounts{}
				}
			}
			if !reflect.DeepEqual(r.Summary, want) {
				t.Fatalf("checked=%t, allSkipped=%t: got %+v, want %+v", checked, allSkipped, r.Summary, want)
			}
			ready := ""
			if checked {
				ready = "READY operators: 1/3"
				if allSkipped {
					ready = "READY operators: 0/0"
				}
			}
			assertSummaryPresentation(t, r, ready)
			payload, err := r.Slack(SlackOptions{Summary: true, Table: true, List: true, Details: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
			if err != nil {
				t.Fatal(err)
			}
			groups := textListGroups(t, r.Text())
			slackGroups, order := slackListGroups(t, payload)
			if !reflect.DeepEqual(groups, slackGroups) || !reflect.DeepEqual(order, []string{"OK", "PLCCDATA", "OPERATOR", "SKIPPED"}) {
				t.Fatalf("inconsistent groups: %v / %v / %v", groups, slackGroups, order)
			}
			var skipped []string
			for _, pkg := range a.Packages {
				if pkg.Action == Skipped {
					skipped = append(skipped, pkg.Name)
					if lines := detailLines(pkg); !reflect.DeepEqual(lines, []string{"[SKIPPED] " + reasons[pkg.Name]}) {
						t.Fatalf("skip details included failures: %v", lines)
					}
				}
			}
			if !reflect.DeepEqual(groups["SKIPPED"], skipped) || len(groups["OK"])+len(groups["PLCCDATA"])+len(groups["OPERATOR"])+len(skipped) != 7 {
				t.Fatalf("lost or duplicated operators: %v", groups)
			}
			encoded, _ := json.Marshal(r)
			var decoded Report
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Summary, decoded.Summary) || !reflect.DeepEqual(a.Packages, decoded.Assessment.Packages) {
				t.Fatal("JSON lost skip metadata or evidence")
			}
		}
	}
}
