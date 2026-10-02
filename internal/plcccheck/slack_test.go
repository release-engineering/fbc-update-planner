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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestSlackSectionsAndFindings(t *testing.T) {
	source, inventory := fixtures(t)
	r := NewReport(mustAssess(t, source, inventory, plcc.DatasetOptions{Packages: []string{"mixed", "stale"}, Validators: []string{"syntax"}}))
	payload, err := r.Slack(SlackOptions{Summary: true, Table: true, List: true, Details: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INCOMPLETE", "plcc-version-missing", "plcc-version-regressed", "Details: mixed", "Details: stale", "rich_text_preformatted", "1/3"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("Slack missing %q: %s", want, data)
		}
	}
	payload, err = r.Slack(SlackOptions{Summary: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(payload)
	if strings.Contains(string(data), "Details:") || strings.Contains(string(data), "rich_text_preformatted") {
		t.Fatal("unrequested Slack sections included")
	}
}

func TestActionPresentation(t *testing.T) {
	source, inventory := fixtures(t)
	assessment := mustAssess(t, source, inventory, plcc.DatasetOptions{Validators: []string{"syntax", "catalog"}})
	if err := assessment.ApplySkips(map[string]string{"validator": "Reporting exception"}); err != nil {
		t.Fatal(err)
	}
	r := NewReport(assessment)
	payload, err := r.Slack(SlackOptions{Table: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	var slack strings.Builder
	for _, block := range payload.Blocks {
		slack.WriteString(slackBlockText(t, block))
	}
	normalized := strings.Join(strings.Fields(slack.String()), " ")
	textTable, _, _ := strings.Cut(r.Text(), "\nList\n")
	normalizedText := strings.Join(strings.Fields(textTable), " ")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ icon, action, label, row string }{
		{"📋", "PLCC add", "PLCCDATA", "bundle-only | MISSING | MISSING"},
		{"📋", "PLCC fix", "PLCCDATA", "mixed | 2/3 | 1/3"},
		{"📦", "OPERATOR add", "OPERATOR", "lifecycle-only | ✅ | NO BUNDLES"},
		{"📦", "OPERATOR build", "OPERATOR", "unpublished | ✅ | MISSING"},
		{"✅", "OK", "", "full | ✅ | ✅"},
		{"➖", "SKIPPED", "", "validator | INVALID | MISSING"},
	} {
		if want := strings.TrimSpace(tt.icon+" "+tt.label) + " | " + tt.row; !strings.Contains(normalized, want) {
			t.Errorf("Slack missing action row %q", want)
		}
		name, _, _ := strings.Cut(tt.row, " | ")
		if want := strings.TrimSpace(tt.icon+" "+tt.label) + " " + name; !strings.Contains(normalizedText, want) {
			t.Errorf("text table missing action row %q", want)
		}
		if !strings.Contains(string(data), `"action":"`+tt.action+`"`) {
			t.Errorf("specific action %q missing from JSON", tt.action)
		}
		if strings.Contains(string(data), tt.icon) {
			t.Errorf("action icon %q leaked into JSON", tt.icon)
		}
	}
	if strings.Contains(string(data), "PLCCDATA") {
		t.Fatal("grouped action label leaked into JSON")
	}
	for _, old := range []string{"PLCC add", "PLCC fix", "OPERATOR add", "OPERATOR build", "✅ OK", "➖ SKIPPED"} {
		if strings.Contains(textTable, old) {
			t.Errorf("text table still uses action label %q", old)
		}
	}
}

func TestSlackLimitsAndLiteralText(t *testing.T) {
	for _, kind := range []string{"many rows", "many packages with details", "oversized row and reason", "unicode", "skip notes"} {
		t.Run(kind, func(t *testing.T) {
			a := &Assessment{CatalogChecked: true}
			for i := 0; i < 2500; i++ {
				pkg := PackageAssessment{Name: fmt.Sprintf("operator-%04d-<!channel>-<tag>-\x60\x60\x60", i), PLCC: PLCCInvalid, Action: FixPLCC}
				switch kind {
				case "many packages with details":
					pkg.Failures = []Failure{{Stage: FBCTranslation, Reasons: []string{"[FBC-VER-01] failure"}}}
				case "oversized row and reason":
					pkg.Name = strings.Repeat("x", 3100)
					pkg.Failures = []Failure{{Stage: FBCTranslation, Reasons: []string{strings.Repeat("x", 3100)}}}
				case "unicode":
					pkg.Name += strings.Repeat("界", 50)
				case "skip notes":
					pkg.Action, pkg.SkipReason = Skipped, strings.Repeat("skip <!channel> | OK ", 200)
				}
				a.Packages = append(a.Packages, pkg)
			}
			opts := SlackOptions{Summary: true, Table: kind != "many packages with details", Details: true, RunURL: "https://github.com/org/repo/actions/runs/42"}
			payload, err := NewReport(a).Slack(opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(payload.Blocks) > 50 {
				t.Fatalf("%d blocks exceeds Slack limit", len(payload.Blocks))
			}
			total := 0
			for i, block := range payload.Blocks {
				value := slackBlockText(t, block)
				n := utf8.RuneCountInString(value)
				total += n
				if n > 3000 || n == 0 || !utf8.ValidString(value) {
					t.Fatalf("invalid block %d length %d", i, n)
				}
				if strings.Contains(value, "<!channel>") && block.Text != nil && block.Text.Type != "plain_text" {
					t.Fatal("source text could become a Slack mention")
				}
			}
			if total > 35000 {
				t.Fatalf("message exceeds character budget: %d", total)
			}
			last := payload.Blocks[len(payload.Blocks)-1].Text.Text
			if !strings.Contains(last, "report lines omitted") || !strings.Contains(last, "summary.txt and assessment.json") || !strings.Contains(last, opts.RunURL) {
				t.Fatalf("missing omission notice/artifact link: %s", last)
			}
		})
	}
}

func TestSkippedTableLiteralNotes(t *testing.T) {
	const name = "operator | OK"
	const note = "Group | OK\n<!channel> `literal`"
	a := &Assessment{Packages: []PackageAssessment{
		{Name: name, PLCC: PLCCOK, Action: Skipped, SkipReason: note},
		{Name: "ordinary", PLCC: PLCCOK, Action: OK},
	}}
	r := NewReport(a)
	rows := r.tableRows()
	if rows[1][0] != "" || rows[1][4] != oneLine(note) || rows[2][4] != "" {
		t.Fatalf("incorrect skip cells: %v", rows)
	}
	lines := r.slackTableLines()
	want := "➖ | operator | OK | ✅ | NOT CHECKED | Group | OK\\n<!channel> `literal`"
	if got := strings.Join(strings.Fields(lines[1]), " "); got != want {
		t.Fatalf("skip table changed literal text or statuses: %q, want %q", got, want)
	}
	if got := detailLines(a.Packages[0]); !reflect.DeepEqual(got, []string{"[SKIPPED] " + oneLine(note)}) {
		t.Fatalf("unescaped note in details: %v", got)
	}
}

// Check the actual Block Kit structure, not just the presence of table text.
func slackBlockText(t *testing.T, block slackBlock) string {
	t.Helper()
	if block.Type != "rich_text" {
		if block.Text == nil || len(block.Elements) != 0 {
			t.Fatalf("invalid text block: %+v", block)
		}
		if block.Type == "header" && (block.Text.Type != "plain_text" || utf8.RuneCountInString(block.Text.Text) > 150) {
			t.Fatalf("invalid Slack header: %+v", block)
		}
		return block.Text.Text
	}
	if block.Text != nil || len(block.Elements) != 1 || block.Elements[0].Type != "rich_text_preformatted" {
		t.Fatalf("block is not preformatted rich text: %+v", block)
	}
	leaves := block.Elements[0].Elements
	if len(leaves) != 1 || leaves[0].Type != "text" {
		t.Fatalf("preformatted block must contain literal text: %+v", leaves)
	}
	return leaves[0].Text
}

func TestSlackTableColumnsAndChunks(t *testing.T) {
	a := &Assessment{}
	actions := []Action{AddPLCC, FixPLCC, AddOperator, BuildOperator, OK}
	for i := range 150 {
		a.Packages = append(a.Packages, PackageAssessment{
			Name: fmt.Sprintf("operator-%03d%s", i, strings.Repeat("x", i%5)),
			PLCC: PLCCOK, Action: actions[i%len(actions)],
		})
	}
	payload, err := NewReport(a).Slack(SlackOptions{Table: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	// Check padding using two display columns for each icon.
	icons := strings.NewReplacer("📋", "  ", "📦", "  ", "✅", "  ")
	var header string
	var rows []string
	chunks := 0
	for _, block := range payload.Blocks {
		if block.Type != "rich_text" {
			continue
		}
		chunks++
		value := slackBlockText(t, block)
		if utf8.RuneCountInString(value) > 3000 {
			t.Fatal("table chunk exceeds character budget")
		}
		lines := strings.Split(value, "\n")
		if header == "" {
			header = lines[0]
		}
		if lines[0] != header || strings.Join(strings.Fields(header), " ") != "ACTION | OPERATOR | PLCC | CATALOG | SKIPPED" {
			t.Fatalf("missing repeated table header: %q", lines[0])
		}
		for _, line := range lines[1:] {
			padded := icons.Replace(line)
			for col, ch := range header {
				if ch == '|' && (len(padded) <= col || padded[col] != '|') {
					t.Errorf("unaligned column at %d: %q (header %q)", col, line, header)
				}
			}
			rows = append(rows, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	if chunks < 2 || len(rows) != len(a.Packages) {
		t.Fatalf("got %d chunks with %d rows, want multiple chunks with %d rows", chunks, len(rows), len(a.Packages))
	}
	for i, row := range rows {
		if row != a.Packages[i].Name {
			t.Errorf("row %d = %q, want %q", i, row, a.Packages[i].Name)
		}
	}
}

func TestSlackTableLiteralNameAndOversizedRow(t *testing.T) {
	const name = "operator-<!channel>-&-```-*literal*"
	a := &Assessment{Packages: []PackageAssessment{
		{Name: strings.Repeat("x", 3100), PLCC: PLCCOK, Action: OK},
		{Name: name, PLCC: PLCCOK, Action: OK},
	}}
	payload, err := NewReport(a).Slack(SlackOptions{Table: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, block := range payload.Blocks {
		if block.Type == "rich_text" && strings.Contains(slackBlockText(t, block), name) {
			found = true
			data, err := json.Marshal(block)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["text"]; ok {
				t.Fatal("rich_text block has an unsupported top-level text field")
			}
		}
	}
	if !found {
		t.Fatal("oversized row hid a short row or literal name was changed")
	}
	if last := slackBlockText(t, payload.Blocks[len(payload.Blocks)-1]); !strings.Contains(last, "1 report lines omitted") {
		t.Fatalf("incorrect omission notice: %q", last)
	}
}

func TestSlackActionLists(t *testing.T) {
	a := &Assessment{CatalogChecked: true, Packages: []PackageAssessment{
		{Name: "z-operator", Action: OK},
		{Name: "comma,operator", Action: FixPLCC},
		{Name: "missing", Action: AddPLCC},
		{Name: "quote\"operator", Action: FixPLCC},
		{Name: "<!channel>-```-operator\nnext", Action: FixPLCC},
		{Name: "new", Action: AddOperator},
		{Name: "rebuild", Action: BuildOperator},
		{Name: "a-operator", Action: OK},
	}}
	r := NewReport(a)
	payload, err := r.Slack(SlackOptions{List: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	groups, order := slackListGroups(t, payload)
	wantOrder := []string{"OK", "PLCCDATA", "OPERATOR", "SKIPPED"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("group order = %v, want %v", order, wantOrder)
	}
	want := map[string][]string{
		"OK":       {"z-operator", "a-operator"},
		"PLCCDATA": {"comma,operator", "missing", "quote\"operator", "<!channel>-```-operator\\nnext"},
		"OPERATOR": {"new", "rebuild"},
		"SKIPPED":  nil,
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("action lists = %#v, want %#v", groups, want)
	}
	if textGroups := textListGroups(t, r.Text()); !reflect.DeepEqual(textGroups, want) {
		t.Fatalf("text CSV lists differ from Slack: %#v, want %#v", textGroups, want)
	}
	for _, heading := range []string{
		"✅ OK - operators ready 2/8",
		"📋 PLCCDATA - operators that need PLCC fixes 4/8",
		"📦 OPERATOR - operators that need a catalog rebuild 2/8 (PLCC data ready, missing bundles or lifecycle data in the catalog)",
		"➖ SKIPPED - operators excluded from action reporting 0/8",
	} {
		found := false
		for _, block := range payload.Blocks {
			if strings.TrimSuffix(slackBlockText(t, block), "\nNone") == heading {
				found = true
			}
		}
		if !found || !strings.Contains(r.Text(), "\n"+heading+"\n") {
			t.Errorf("missing list heading in Slack or text: %q", heading)
		}
	}
	for _, block := range payload.Blocks {
		if value := slackBlockText(t, block); strings.Contains(value, "Catalog X/Y") || strings.Contains(value, "catalog not checked") {
			t.Fatalf("unexpected legend or qualifier: %q", value)
		}
	}
}

func TestSlackEmptyActionLists(t *testing.T) {
	r := NewReport(&Assessment{})
	payload, err := r.Slack(SlackOptions{List: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
	if err != nil {
		t.Fatal(err)
	}
	groups, order := slackListGroups(t, payload)
	if len(order) != 4 || len(groups) != 4 {
		t.Fatalf("missing empty action groups: %v / %v", order, groups)
	}
	for action, names := range groups {
		if len(names) != 0 {
			t.Errorf("empty group %s contains names: %v", action, names)
		}
	}
	if text := slackBlockText(t, payload.Blocks[2]); text != "✅ OK - operators with PLCC data ready 0/0 (catalog not checked)\nNone" {
		t.Fatalf("missing unchecked-catalog qualifier: %q", text)
	}
	textGroups := textListGroups(t, r.Text())
	for _, heading := range []string{"OK", "PLCCDATA", "OPERATOR", "SKIPPED"} {
		if names, ok := textGroups[heading]; !ok || len(names) != 0 {
			t.Errorf("missing empty text group %q", heading)
		}
	}
	for _, block := range payload.Blocks {
		value := slackBlockText(t, block)
		if strings.HasSuffix(value, "\nNone") && !strings.Contains(value, " 0/0") {
			t.Errorf("missing zero count in empty list: %q", value)
		}
	}
}

// Read each copyable block as a standalone CSV record and associate it with
// the preceding action heading. Empty groups have no CSV block.
func slackListGroups(t *testing.T, payload *SlackPayload) (map[string][]string, []string) {
	t.Helper()
	groups := make(map[string][]string)
	var order []string
	for i, block := range payload.Blocks {
		if block.Text == nil || block.Text.Type != "plain_text" {
			continue
		}
		value := slackBlockText(t, block)
		for _, heading := range []string{"✅ OK", "📋 PLCCDATA", "📦 OPERATOR", "➖ SKIPPED"} {
			if !strings.HasPrefix(value, heading) {
				continue
			}
			_, action, _ := strings.Cut(heading, " ")
			order = append(order, action)
			if strings.HasSuffix(value, "\nNone") {
				groups[action] = nil
				continue
			}
			if i+1 >= len(payload.Blocks) || payload.Blocks[i+1].Type != "rich_text" {
				t.Fatalf("action heading without CSV block: %q", value)
			}
			if _, continued := groups[action]; continued != strings.HasSuffix(value, " (continued)") {
				t.Fatalf("incorrect continuation heading: %q", value)
			}
			records, err := csv.NewReader(strings.NewReader(slackBlockText(t, payload.Blocks[i+1]))).ReadAll()
			if err != nil || len(records) != 1 {
				t.Fatalf("invalid CSV record after %q: %v / %v", value, records, err)
			}
			groups[action] = append(groups[action], records[0]...)
		}
	}
	return groups, order
}

func TestSlackListChunksAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name             string
		count, length    int
		oversized, table bool
	}{
		{name: "complete across chunks", count: 150, length: 30},
		{name: "message character limit", count: 1500, length: 30},
		{name: "shared table and list budget", count: 47, length: 800, table: true},
		{name: "oversized field", count: 1, oversized: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Assessment{}
			if tc.oversized {
				a.Packages = append(a.Packages, PackageAssessment{Name: strings.Repeat("界", 3001), Action: FixPLCC})
			}
			for i := range tc.count {
				a.Packages = append(a.Packages, PackageAssessment{
					Name: fmt.Sprintf("operator-%04d-", i) + strings.Repeat("界", tc.length), Action: FixPLCC,
				})
			}
			payload, err := NewReport(a).Slack(SlackOptions{Table: tc.table, List: true, RunURL: "https://github.com/org/repo/actions/runs/42"})
			if err != nil {
				t.Fatal(err)
			}
			groups, order := slackListGroups(t, payload)
			shown := groups["PLCCDATA"]
			start := 0
			if tc.oversized {
				start = 1
			}
			for i, name := range shown {
				if name != a.Packages[i+start].Name {
					t.Fatalf("lost, duplicated, or changed name at %d: %q", i, name)
				}
			}
			omitted := len(a.Packages) - len(shown)
			footer := slackBlockText(t, payload.Blocks[len(payload.Blocks)-1])
			if omitted > 0 {
				if !strings.Contains(footer, fmt.Sprintf("%d operator names omitted from action lists", omitted)) {
					t.Fatalf("incorrect omitted-name count: %q", footer)
				}
			} else if strings.Contains(footer, "omitted") {
				t.Fatalf("unexpected omission notice: %q", footer)
			}
			if tc.name == "complete across chunks" && (len(shown) != tc.count || len(order) <= 4) {
				t.Fatalf("expected all names across multiple chunks, got %d names and %d headings", len(shown), len(order))
			}
			if tc.name == "message character limit" {
				complete := textListGroups(t, NewReport(a).Text())["PLCCDATA"]
				if len(complete) != tc.count {
					t.Fatalf("text CSV truncated to %d operators", len(complete))
				}
			}
			if tc.oversized && len(shown) != 1 {
				t.Fatal("oversized field hid a short name")
			}
			total := 0
			for _, block := range payload.Blocks {
				value := slackBlockText(t, block)
				if strings.HasPrefix(value, "📋 PLCCDATA") {
					want := fmt.Sprintf("📋 PLCCDATA - operators that need PLCC fixes %d/%d", len(a.Packages), len(a.Packages))
					if strings.TrimSuffix(value, " (continued)") != want {
						t.Fatalf("list heading lost full group count: %q", value)
					}
				}
				n := utf8.RuneCountInString(value)
				if n == 0 || n > 3000 || !utf8.ValidString(value) {
					t.Fatalf("invalid block length or encoding: %d", n)
				}
				total += n
			}
			if total > 35000 || len(payload.Blocks) > 50 {
				t.Fatalf("message exceeds limits: %d characters, %d blocks", total, len(payload.Blocks))
			}
		})
	}
}

func TestSlackListReservesHeadingAndFooter(t *testing.T) {
	for _, occupied := range []int{47, 48} {
		b := slackBuilder{payload: &SlackPayload{}, remaining: 10000}
		for range occupied {
			b.payload.Blocks = append(b.payload.Blocks, slackTextBlock("existing content", false))
		}
		b.addList("📋 PLCCDATA", []string{strings.Repeat("a", 2000), strings.Repeat("b", 2000)})
		wantShown := 49 - occupied
		if occupied == 48 {
			wantShown = 0
		}
		if len(b.payload.Blocks) != occupied+wantShown || b.omittedNames != 2-wantShown/2 {
			t.Fatalf("with %d occupied blocks: got %d blocks and %d omitted names", occupied, len(b.payload.Blocks), b.omittedNames)
		}
	}
}

func TestSlackCoverageLegendFollowsSectionSelection(t *testing.T) {
	source, inventory := fixtures(t)
	r := NewReport(mustAssess(t, source, inventory, plcc.DatasetOptions{Validators: []string{"syntax"}}))
	for _, opts := range []SlackOptions{{Summary: true}, {Table: true}, {List: true}, {Details: true}, {Summary: true, Table: true, List: true, Details: true}} {
		opts.RunURL = "https://github.com/org/repo/actions/runs/42"
		payload, err := r.Slack(opts)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, block := range payload.Blocks {
			value := slackBlockText(t, block)
			if strings.Contains(value, "Catalog X/Y") {
				found = true
				if value != tableCoverageLegend {
					t.Fatalf("coverage legend misplaced: %q", value)
				}
			}
		}
		if found != opts.Table {
			t.Errorf("legend present = %t for options %+v", found, opts)
		}
	}
}

func TestSlackRejectsBadArtifactURLs(t *testing.T) {
	for _, value := range []string{"", "relative", "javascript:alert(1)", "https://example.com/|fake>", "https://user:secret@example.com", "https://example.com/" + strings.Repeat("x", 3000)} {
		if _, err := NewReport(&Assessment{}).Slack(SlackOptions{RunURL: value}); err == nil {
			t.Errorf("accepted URL %q", value)
		}
	}
}
