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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"
)

// Summary counts operators, never individual failures. Catalog is nil when the
// run did not check a catalog, including runs with no selected operators.
// PLCC, Catalog, and FullyOK count only non-skipped operators.
type Summary struct {
	Total      int            `json:"total"`
	NonSkipped int            `json:"nonSkipped"`
	Skipped    int            `json:"skipped"`
	PLCC       PLCCCounts     `json:"plcc"`
	Catalog    *CatalogCounts `json:"catalog,omitempty"`
	FullyOK    int            `json:"fullyOK"`
}

type PLCCCounts struct {
	OK         int `json:"ok"`
	Missing    int `json:"missing"`
	Incomplete int `json:"incomplete"`
	Invalid    int `json:"invalid"`
	Regressed  int `json:"regressed"`
	Duplicate  int `json:"duplicate"`
}

type CatalogCounts struct {
	OK         int `json:"ok"`
	Missing    int `json:"missing"`
	Incomplete int `json:"incomplete"`
	NoBundles  int `json:"noBundles"`
}

// Report is the common input to text, JSON and Slack rendering. Assessment and
// its pipeline outputs remain caller-owned; do not mutate them while rendering.
type Report struct {
	Summary      Summary     `json:"summary"`
	Assessment   *Assessment `json:"assessment"`
	CatalogImage string      `json:"catalogImage,omitempty"`
	CatalogInput string      `json:"catalogInput,omitempty"`
}

func NewReport(assessment *Assessment) *Report {
	r := &Report{Assessment: assessment}
	r.Summary.Total = len(assessment.Packages)
	if assessment.CatalogChecked {
		r.Summary.Catalog = &CatalogCounts{}
	}
	for _, pkg := range assessment.Packages {
		if pkg.Action == Skipped {
			r.Summary.Skipped++
			continue
		}
		r.Summary.NonSkipped++
		switch pkg.PLCC {
		case PLCCOK:
			r.Summary.PLCC.OK++
		case PLCCAbsent:
			r.Summary.PLCC.Missing++
		case PLCCDuplicate:
			r.Summary.PLCC.Duplicate++
		case PLCCInvalid:
			r.Summary.PLCC.Invalid++
		case PLCCRegressed:
			r.Summary.PLCC.Regressed++
		case PLCCIncomplete:
			r.Summary.PLCC.Incomplete++
		}
		if pkg.Catalog == nil {
			continue
		}
		switch pkg.Catalog.Status() {
		case "OK":
			r.Summary.Catalog.OK++
			if pkg.PLCC == PLCCOK {
				r.Summary.FullyOK++
			}
		case "MISSING":
			r.Summary.Catalog.Missing++
		case "NO BUNDLES":
			r.Summary.Catalog.NoBundles++
		default:
			r.Summary.Catalog.Incomplete++
		}
	}
	return r
}

const tableCoverageLegend = "PLCC/Catalog X/Y: X versions available, Y versions required."

// Text renders the full summary and all findings, grouped by operator.
func (r *Report) Text() string {
	var out strings.Builder
	out.WriteString("Operator lifecycle assessment\n\n")
	out.WriteString(r.summaryText())
	out.WriteByte('\n')
	if r.Assessment.CatalogChecked {
		fmt.Fprintln(&out, tableCoverageLegend)
	}
	var tableText strings.Builder
	table := tabwriter.NewWriter(&tableText, 0, 4, 2, ' ', 0)
	for _, row := range r.tableRows() {
		// strings.Builder writes cannot fail.
		_, _ = fmt.Fprintln(table, strings.Join(row, "\t"))
	}
	_ = table.Flush()
	for i, line := range strings.Split(strings.TrimSuffix(tableText.String(), "\n"), "\n") {
		// Add icons after padding, reserving two display columns per icon.
		icon := "  "
		if i > 0 {
			icon = actionIcon(r.Assessment.Packages[i-1].Action)
		}
		fmt.Fprintln(&out, strings.TrimRight(icon+" "+line, " "))
	}
	out.WriteString("\nList\n")
	for _, list := range r.actionLists() {
		fmt.Fprintf(&out, "\n%s %s\n", actionIcon(list.action), list.heading)
		if len(list.names) == 0 {
			out.WriteString("None\n")
		} else {
			fmt.Fprintln(&out, csvRecord(list.names))
		}
	}
	out.WriteString("\nDetails\n")
	found := false
	for _, pkg := range r.Assessment.Packages {
		lines := detailLines(pkg)
		if len(lines) == 0 {
			continue
		}
		found = true
		fmt.Fprintf(&out, "\n%s\n", oneLine(pkg.Name))
		for _, line := range lines {
			fmt.Fprintf(&out, "  - %s\n", line)
		}
	}
	if !found {
		out.WriteString("  No findings.\n")
	}
	return out.String()
}

func (r *Report) summaryText() string {
	overview, ready, statuses := r.summaryParts()
	lines := append([]string{"Summary"}, overview...)
	if ready != "" {
		lines = append(lines, ready)
	}
	lines = append(lines, statuses...)
	return strings.Join(lines, "\n") + "\n"
}

// Keep summary wording and order shared, with readiness separate so Slack can
// emphasize it without interpreting source references as formatted text.
func (r *Report) summaryParts() (overview []string, ready string, statuses []string) {
	s := r.Summary
	if r.CatalogImage != "" {
		overview = append(overview, "Catalog image: "+oneLine(r.CatalogImage), "")
	} else if r.CatalogInput != "" {
		overview = append(overview, "Catalog input: "+oneLine(r.CatalogInput), "")
	}
	overview = append(overview,
		fmt.Sprintf("Total operators: %d", s.Total),
		fmt.Sprintf("Skipped operators: %d    (Status counts exclude skipped operators)", s.Skipped))
	statuses = append(statuses, fmt.Sprintf("PLCC: OK %d | MISSING %d | INCOMPLETE %d | INVALID %d | REGRESSED %d | DUPLICATE %d",
		s.PLCC.OK, s.PLCC.Missing, s.PLCC.Incomplete, s.PLCC.Invalid, s.PLCC.Regressed, s.PLCC.Duplicate))
	if c := s.Catalog; c != nil {
		ready = fmt.Sprintf("READY operators: %d/%d", s.FullyOK, s.NonSkipped)
		statuses = append(statuses, fmt.Sprintf("Catalog: OK %d | MISSING %d | INCOMPLETE %d | NO BUNDLES %d", c.OK, c.Missing, c.Incomplete, c.NoBundles))
	} else {
		statuses = append(statuses, "Catalog: NOT CHECKED", "OK means: PLCC OK; catalog not checked.")
	}
	return overview, ready, statuses
}

type actionList struct {
	action  Action
	heading string
	names   []string
}

// Use the same grouping and ordering for the complete text report and Slack.
func (r *Report) actionLists() []actionList {
	groups := make(map[string][]string)
	for _, pkg := range r.Assessment.Packages {
		group := actionGroup(pkg.Action)
		groups[group] = append(groups[group], pkg.Name)
	}
	var lists []actionList
	for _, action := range []Action{OK, FixPLCC, BuildOperator, Skipped} {
		group := actionGroup(action)
		list := actionList{action: action, names: groups[group]}
		var description, qualifier string
		switch action {
		case OK:
			description = "operators ready"
			if !r.Assessment.CatalogChecked {
				description = "operators with PLCC data ready"
				qualifier = " (catalog not checked)"
			}
		case FixPLCC:
			description = "operators that need PLCC fixes"
		case BuildOperator:
			description = "operators that need a catalog rebuild"
			qualifier = " (PLCC data ready, missing bundles or lifecycle data in the catalog)"
		case Skipped:
			description = "operators excluded from action reporting"
		}
		list.heading = fmt.Sprintf("%s - %s %d/%d%s", group, description, len(list.names), r.Summary.Total, qualifier)
		lists = append(lists, list)
	}
	return lists
}

func actionGroup(action Action) string {
	switch action {
	case AddPLCC, FixPLCC:
		return "PLCCDATA"
	case AddOperator, BuildOperator:
		return "OPERATOR"
	default:
		return string(action)
	}
}

func csvRecord(names []string) string {
	fields := make([]string, len(names))
	for i, name := range names {
		fields[i] = oneLine(name)
	}
	var encoded strings.Builder
	writer := csv.NewWriter(&encoded)
	// strings.Builder writes cannot fail.
	_ = writer.Write(fields)
	writer.Flush()
	return strings.TrimSuffix(encoded.String(), "\n")
}

func (r *Report) tableRows() [][]string {
	rows := [][]string{{"ACTION", "OPERATOR", "PLCC", "CATALOG", "SKIPPED"}}
	for _, pkg := range r.Assessment.Packages {
		status := "NOT CHECKED"
		if pkg.Catalog != nil {
			status = pkg.Catalog.Status()
		}
		action := ""
		if pkg.Action != OK && pkg.Action != Skipped {
			action = actionGroup(pkg.Action)
		}
		rows = append(rows, []string{action, oneLine(pkg.Name), pkg.plccTableStatus(), status, oneLine(pkg.SkipReason)})
	}
	return rows
}

func (p PackageAssessment) plccTableStatus() string {
	if p.PLCC != PLCCIncomplete || p.Catalog == nil || len(p.Catalog.BundleVersions) == 0 {
		return string(p.PLCC)
	}
	covered := 0
	for _, version := range p.Catalog.BundleVersions {
		if slices.Contains(p.SourceVersions, version.String()) {
			covered++
		}
	}
	return fmt.Sprintf("%d/%d", covered, len(p.Catalog.BundleVersions))
}

func detailLines(pkg PackageAssessment) []string {
	if pkg.Action == Skipped {
		return []string{"[SKIPPED] " + oneLine(pkg.SkipReason)}
	}
	var lines []string
	for _, failure := range pkg.Failures {
		label := string(failure.Stage)
		if failure.Validator != nil {
			label += "; " + failure.Validator.Label
		}
		label += fmt.Sprintf("; source product %d; targets %s", failure.SourceIndex, strings.Join(failure.Packages, ", "))
		for _, reason := range failure.Reasons {
			lines = append(lines, "["+oneLine(label)+"] "+oneLine(reason))
		}
	}
	missingLifecycle := pkg.hasIssue(MissingCatalogLifecycle)
	for _, issue := range pkg.Issues {
		version := ""
		if issue.Version != nil {
			version = issue.Version.String()
		}
		var message string
		switch issue.Kind {
		case MissingPLCCPackage:
			message = "Package is absent from source PLCC."
		case MissingPLCCVersion:
			message = fmt.Sprintf("PLCC version %s is missing; required by catalog bundles.", version)
		case RegressedPLCCVersion:
			message = fmt.Sprintf("PLCC version %s is missing but its lifecycle data is already shipped in the catalog.", version)
		case MissingCatalogBundles:
			message = "No operator bundles are present in the catalog."
		case MissingCatalogLifecycle:
			message = "No lifecycle entry is present in the catalog."
		case MissingCatalogLifecycleVersion:
			if missingLifecycle {
				continue
			}
			message = fmt.Sprintf("Catalog lifecycle version %s is missing; required by catalog bundles.", version)
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", issue.Kind, message))
	}
	return lines
}

// Keep each finding and table row on one line without discarding source text.
// JSON retains the original strings verbatim.
func oneLine(s string) string {
	var out strings.Builder
	for _, ch := range s {
		if unicode.IsControl(ch) || ch == '\u2028' || ch == '\u2029' {
			quoted := strconv.QuoteRune(ch)
			out.WriteString(quoted[1 : len(quoted)-1])
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String()
}
