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
	"net/url"
	"strings"
	"unicode/utf8"
)

// SlackOptions selects sections without changing the full text/JSON artifacts.
// RunURL links to the workflow artifacts. This renderer never posts messages.
type SlackOptions struct {
	Summary bool
	Table   bool
	List    bool
	Details bool
	Scope   string
	RunURL  string
}

type SlackPayload struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string              `json:"type"`
	Text     *slackText          `json:"text,omitempty"`
	Elements []slackPreformatted `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackPreformatted struct {
	Type     string      `json:"type"`
	Elements []slackText `json:"elements"`
}

type slackBuilder struct {
	payload      *SlackPayload
	remaining    int
	omittedLines int
	omittedNames int
}

// Slack bounds both sections and the complete message. Oversized lines and
// content exceeding the budget are omitted with an explicit notice and link.
// See https://docs.slack.dev/reference/block-kit/blocks/section-block/ (3000
// characters per section) and /blocks/ (50 blocks per message).
func (r *Report) Slack(options SlackOptions) (*SlackPayload, error) {
	u, err := url.Parse(options.RunURL)
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") ||
		u.User != nil || strings.ContainsAny(options.RunURL, "<>| \t\r\n") {
		return nil, fmt.Errorf("Slack report requires an absolute HTTP(S) workflow run URL")
	}
	footer := "<" + strings.ReplaceAll(u.String(), "&", "&amp;") + "|Open workflow run and download full artifacts>"
	if utf8.RuneCountInString(footer) > 2500 {
		return nil, fmt.Errorf("workflow run URL is too long for Slack")
	}
	payload := &SlackPayload{
		Text:   "Operator lifecycle assessment. " + u.String(),
		Blocks: []slackBlock{{Type: "header", Text: &slackText{Type: "plain_text", Text: "Operator lifecycle assessment"}}},
	}
	// Leave room for the header, omission notice, and artifact link. A total
	// character budget also avoids a message made of 50 nearly-full sections.
	b := slackBuilder{payload: payload, remaining: 35000 - utf8.RuneCountInString(footer) - 500}
	if options.Scope != "" {
		b.addLines("Scope", []string{oneLine(options.Scope)}, false)
	}
	if options.Summary {
		overview, ready, statuses := r.summaryParts()
		b.addHeader("Summary")
		b.addLines("", overview, false)
		if ready != "" {
			b.addHeader(ready)
		}
		b.addLines("", statuses, false)
	}
	if options.Table {
		b.addHeader("Table")
		if r.Assessment.CatalogChecked {
			b.addLines("", []string{tableCoverageLegend}, false)
		}
		lines := r.slackTableLines()
		b.addLines(lines[0], lines[1:], true)
	}
	if options.List {
		b.addHeader("List")
		for _, list := range r.actionLists() {
			b.addList(actionIcon(list.action)+" "+list.heading, list.names)
		}
	}
	if options.Details {
		b.addHeader("Details")
		hasFindings := false
		for _, pkg := range r.Assessment.Packages {
			lines := detailLines(pkg)
			if len(lines) != 0 {
				hasFindings = true
				b.addLines("Details: "+pkg.Name, lines, false)
			}
		}
		if !hasFindings {
			b.addLines("", []string{"No findings."}, false)
		}
	}
	var omissions []string
	if b.omittedLines > 0 {
		omissions = append(omissions, fmt.Sprintf("%d report lines omitted", b.omittedLines))
	}
	if b.omittedNames > 0 {
		omissions = append(omissions, fmt.Sprintf("%d operator names omitted from action lists", b.omittedNames))
	}
	if len(omissions) > 0 {
		footer = strings.Join(omissions, "; ") + " due to Slack message size limits. Full summary and details are in summary.txt and assessment.json.\n" + footer
	}
	payload.Blocks = append(payload.Blocks, slackBlock{Type: "section", Text: &slackText{Type: "mrkdwn", Text: footer}})
	return payload, nil
}

func slackTextBlock(value string, preformatted bool) slackBlock {
	if preformatted {
		// Structured text preserves padding and literal backticks without
		// interpreting source names as Markdown or Slack mentions.
		return slackBlock{Type: "rich_text", Elements: []slackPreformatted{{
			Type: "rich_text_preformatted", Elements: []slackText{{Type: "text", Text: value}},
		}}}
	}
	return slackBlock{Type: "section", Text: &slackText{Type: "plain_text", Text: value}}
}

func (b *slackBuilder) addLines(heading string, lines []string, preformatted bool) {
	prefix := ""
	if heading != "" {
		prefix = oneLine(heading) + "\n"
	}
	prefixSize := utf8.RuneCountInString(prefix)
	chunk, size := prefix, prefixSize
	flush := func() {
		if size == prefixSize {
			return
		}
		b.payload.Blocks = append(b.payload.Blocks, slackTextBlock(strings.TrimSuffix(chunk, "\n"), preformatted))
		b.remaining -= size
		chunk, size = prefix, prefixSize
	}
	for _, line := range lines {
		n := utf8.RuneCountInString(line) + 1
		if size+n > 3000 {
			flush()
		}
		if prefixSize+n > 3000 || len(b.payload.Blocks) >= 49 || size+n > b.remaining {
			b.omittedLines++
			continue
		}
		chunk += line + "\n"
		size += n
	}
	flush()
}

// Section labels and readiness counts fit within Slack's 150-character header
// limit. Reserve a content block and the final footer before adding a header.
func (b *slackBuilder) addHeader(heading string) {
	size := utf8.RuneCountInString(heading)
	if len(b.payload.Blocks) >= 48 || size >= b.remaining {
		b.omittedLines++
		return
	}
	b.payload.Blocks = append(b.payload.Blocks, slackBlock{Type: "header", Text: &slackText{Type: "plain_text", Text: heading}})
	b.remaining -= size
}

// Each chunk is a complete CSV record. Keep the heading outside the code block
// so copying its contents produces only operator names. Account for both blocks
// before admitting names, reserving the final message block for the footer.
func (b *slackBuilder) addList(heading string, names []string) {
	if len(names) == 0 {
		b.addLines(heading, []string{"None"}, false)
		return
	}
	label := heading
	var fields []string
	size := 0
	flush := func() {
		if len(fields) == 0 {
			return
		}
		b.payload.Blocks = append(b.payload.Blocks, slackTextBlock(label, false), slackTextBlock(strings.Join(fields, ","), true))
		b.remaining -= utf8.RuneCountInString(label) + size
		label = heading + " (continued)"
		fields, size = nil, 0
	}
	for _, name := range names {
		field := csvRecord([]string{name})
		n := utf8.RuneCountInString(field)
		if n > 3000 {
			b.omittedNames++
			continue
		}
		extra := n
		if len(fields) > 0 {
			extra++ // comma separating fields
		}
		if size+extra > 3000 || utf8.RuneCountInString(label)+size+extra > b.remaining {
			flush()
			extra = n
		}
		if len(b.payload.Blocks) > 47 || utf8.RuneCountInString(label)+size+extra > b.remaining {
			b.omittedNames++
			continue
		}
		fields = append(fields, field)
		size += extra
	}
	flush()
}

// Measure plain cells before adding icons, reserving two display columns for
// each icon. Keep cells structured: names and skip notes may contain pipes or
// "OK", which must not be interpreted as separators or status values.
func (r *Report) slackTableLines() []string {
	rows := r.tableRows()
	widths := make([]int, len(rows[0])-1)
	for _, row := range rows {
		// An oversized name or note must not widen otherwise usable rows.
		if utf8.RuneCountInString(strings.Join(row, " | "))+3 > 3000 {
			continue
		}
		for column := range widths {
			widths[column] = max(widths[column], utf8.RuneCountInString(row[column]))
		}
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		for column, value := range row {
			size := utf8.RuneCountInString(value)
			if i > 0 && (column == 2 || column == 3) && value == "OK" {
				value = "✅"
			}
			if column < len(widths) {
				value += strings.Repeat(" ", max(0, widths[column]-size))
			}
			row[column] = value
		}
		icon := "  "
		if i > 0 {
			icon = actionIcon(r.Assessment.Packages[i-1].Action)
		}
		lines[i] = strings.TrimRight(icon+" "+strings.Join(row, " | "), " ")
	}
	return lines
}

func actionIcon(action Action) string {
	switch action {
	case AddPLCC, FixPLCC:
		return "📋"
	case AddOperator, BuildOperator:
		return "📦"
	case OK:
		return "✅"
	case Skipped:
		return "➖"
	default:
		return "  "
	}
}
