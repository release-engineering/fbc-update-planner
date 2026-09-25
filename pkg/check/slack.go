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

package check

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/release-engineering/fbc-update-planner/pkg/classify"
)

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackBlock struct {
	Type string    `json:"type"`
	Text slackText `json:"text"`
}

type slackPayload struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

func section(markdown string) slackBlock {
	return slackBlock{Type: "section", Text: slackText{Type: "mrkdwn", Text: markdown}}
}

func renderSlack(data runData) slackPayload {
	heading := "Operator lifecycle assessment"
	if data.options.Operators != "" {
		heading += " (" + filepath.Base(data.options.Operators) + ")"
	}
	blocks := []slackBlock{{Type: "header", Text: slackText{Type: "plain_text", Text: heading}}}
	showSummary := strings.Contains(","+data.options.Webhook+",", ",summary,")
	showList := strings.Contains(","+data.options.Webhook+",", ",list,")
	if showSummary {
		blocks = append(blocks, section(slackSummary(data)))
		if data.options.CatalogImage != "" {
			blocks = append(blocks, section("*Operators ready in PLCC and catalog*"))
			blocks = append(blocks, chunkBlocks(readyLines(data), false)...)
		}
	}
	if showList {
		blocks = append(blocks, section("*Requested operators*"))
		blocks = append(blocks, chunkBlocks(operatorLines(data), true)...)
	}
	if data.options.CatalogImage != "" && (showSummary || showList) {
		blocks = append(blocks, section("*Action details*"))
		blocks = append(blocks, chunkBlocks(actionLines(data.reports), false)...)
	}
	link := section("<" + data.runURL + "|Open workflow run and download artifacts>")
	if len(blocks)+1 > 50 {
		blocks = append(blocks[:48], section("More operator and version details are in the complete artifacts."))
	}
	blocks = append(blocks, link)
	return slackPayload{Text: heading + ". " + data.runURL, Blocks: blocks}
}

func slackSummary(data runData) string {
	var b strings.Builder
	total := len(data.reports)
	b.WriteString("*Summary*\n")
	if data.options.CatalogImage != "" {
		fmt.Fprintf(&b, "*Ready in PLCC and catalog: %d / %d*\n", countReady(data.reports), total)
	}
	scope := "All operators"
	if data.options.Operators != "" {
		scope = "Selected operators"
	}
	fmt.Fprintf(&b, "• Scope: %s\n• Operators assessed: %d\n", scope, total)
	for _, entry := range []struct {
		label string
		count int
	}{
		{"PLCC valid", countPLCC(data.reports, classify.PLCCStatusOK)},
		{"PLCC duplicate", countPLCC(data.reports, classify.PLCCStatusDuplicate)},
		{"PLCC invalid", countPLCC(data.reports, classify.PLCCStatusInvalid)},
		{"PLCC missing", countPLCC(data.reports, classify.PLCCStatusMissing)},
	} {
		fmt.Fprintf(&b, "• %s: %d / %d\n", entry.label, entry.count, total)
	}
	if data.options.CatalogImage != "" {
		for _, entry := range []struct {
			label string
			count int
		}{
			{"Catalog OK", countCatalog(data.reports, "OK")},
			{"Catalog partial", countCatalog(data.reports, "PARTIAL")},
			{"Catalog missing", countCatalog(data.reports, "MISSING")},
		} {
			fmt.Fprintf(&b, "• %s: %d / %d\n", entry.label, entry.count, total)
		}
		b.WriteString("\n*Actions*\n")
		for _, action := range []classify.Action{
			classify.ActionFixPLCC, classify.ActionPLCCMissing, classify.ActionNoCatalogBundles,
			classify.ActionNeedsRebuild, classify.ActionOK,
		} {
			fmt.Fprintf(&b, "• %s: %d / %d\n", action, countAction(data.reports, action), total)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func readyLines(data runData) []string {
	byName := reportIndex(data.reports)
	var lines []string
	for _, name := range data.names {
		r := byName[name]
		if r.PLCC == classify.PLCCStatusOK && r.CatalogStatus == classify.CatalogStatusOK {
			lines = append(lines, "- `"+name+"`")
		}
	}
	if len(lines) == 0 {
		return []string{"_None_"}
	}
	return lines
}

func operatorLines(data runData) []string {
	byName := reportIndex(data.reports)
	maxName := 0
	for _, name := range data.names {
		if len(name) > maxName {
			maxName = len(name)
		}
	}
	var lines []string
	for _, name := range data.names {
		r := byName[name]
		if data.options.CatalogImage == "" {
			lines = append(lines, fmt.Sprintf("%-*s  PLCC: %s", maxName, name, r.PLCC))
			continue
		}
		marker := "  "
		if r.PLCC == classify.PLCCStatusOK && r.CatalogStatus == classify.CatalogStatusOK {
			marker = "✅"
		}
		lines = append(lines, fmt.Sprintf("%s  %-*s  PLCC: %-9s  Catalog: %-9s  %s", marker, maxName, name, r.PLCC, r.CatalogStatus, r.PrimaryAction))
	}
	if len(lines) == 0 {
		return []string{"_None_"}
	}
	return lines
}

func actionLines(reports []classify.OperatorReport) []string {
	var lines []string
	for _, action := range []classify.Action{classify.ActionFixPLCC, classify.ActionPLCCMissing, classify.ActionNeedsRebuild} {
		var entries []string
		for _, r := range reports {
			versions := actionVersions(r, action)
			if len(versions) == 0 && r.PrimaryAction != action {
				continue
			}
			detail := strings.Join(versions, ", ")
			if detail == "" {
				if action == classify.ActionPLCCMissing {
					detail = "package absent from PLCC"
				} else {
					detail = "package-level issue"
				}
			}
			entries = append(entries, "• `"+r.Package+": "+detail+"`")
		}
		if len(entries) > 0 {
			lines = append(lines, "*"+string(action)+"*")
			lines = append(lines, entries...)
			lines = append(lines, "")
		}
	}
	if len(lines) == 0 {
		return []string{"_No action required_"}
	}
	return lines
}

func reportIndex(reports []classify.OperatorReport) map[string]classify.OperatorReport {
	index := make(map[string]classify.OperatorReport, len(reports))
	for _, r := range reports {
		index[r.Package] = r
	}
	return index
}

// chunkBlocks respects Slack's 3000-character section limit and keeps the
// complete report available through the linked artifacts when a row is long.
func chunkBlocks(lines []string, code bool) []slackBlock {
	var blocks []slackBlock
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		content := current.String()
		if code {
			content = "```\n" + content + "\n```"
		}
		blocks = append(blocks, section(content))
		current.Reset()
	}
	for _, line := range lines {
		if len(line) > 2600 {
			cut := 2500
			for !utf8.ValidString(line[:cut]) {
				cut--
			}
			line = line[:cut] + "… (see complete artifacts)"
		}
		needed := len(line)
		if current.Len() > 0 {
			needed++
		}
		if current.Len()+needed > 2800 {
			flush()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	flush()
	return blocks
}
