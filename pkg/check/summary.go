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

	"github.com/release-engineering/fbc-update-planner/pkg/classify"
)

func renderSummary(data runData) string {
	var b strings.Builder
	count := "all"
	if data.options.Operators != "" {
		count = fmt.Sprint(len(data.names))
	}
	if data.options.ValidateOnly {
		fmt.Fprintf(&b, "Running plcc-check with %s operators (PLCC validation only)...\n", count)
	} else {
		fmt.Fprintf(&b, "Running plcc-check with %s operators...\n", count)
	}
	if data.options.CatalogImage != "" {
		fmt.Fprintf(&b, "Fetching catalog package list from %s...\n", data.options.CatalogImage)
	}
	if !data.options.ValidateOnly && !data.hasFBC {
		b.WriteString("Warning: no FBC data generated\n")
	}
	byName := make(map[string]classify.OperatorReport, len(data.reports))
	for _, r := range data.reports {
		byName[r.Package] = r
	}

	b.WriteString("\n=== Requested operators ===\n")
	if data.options.CatalogImage != "" {
		b.WriteString("     PLCC       CATALOG    OPERATOR  ACTION\n")
	} else {
		b.WriteString("     PLCC       OPERATOR\n")
	}
	for _, name := range data.names {
		r := byName[name]
		marker := " "
		if r.PLCC == classify.PLCCStatusOK && (data.options.CatalogImage == "" || r.CatalogStatus == classify.CatalogStatusOK) {
			marker = "*"
		}
		if data.options.CatalogImage != "" {
			fmt.Fprintf(&b, "  %s  %-9s  %-9s  %s  %s\n", marker, r.PLCC, r.CatalogStatus, name, r.PrimaryAction)
		} else {
			fmt.Fprintf(&b, "  %s  %-9s  %s\n", marker, r.PLCC, name)
		}
	}

	total := len(data.names)
	b.WriteString("\n=== Summary ===\n")
	fmt.Fprintf(&b, "  %-18s %d\n", "Total operators:", total)
	for _, entry := range []struct {
		label  string
		status classify.PLCCStatus
	}{
		{"PLCC OK:", classify.PLCCStatusOK},
		{"PLCC DUPLICATE:", classify.PLCCStatusDuplicate},
		{"PLCC INVALID:", classify.PLCCStatusInvalid},
		{"PLCC MISSING:", classify.PLCCStatusMissing},
	} {
		fmt.Fprintf(&b, "  %-18s %d / %d\n", entry.label, countPLCC(data.reports, entry.status), total)
	}
	if data.options.CatalogImage != "" {
		fmt.Fprintf(&b, "  %-18s %d / %d\n", "CATALOG OK:", countCatalog(data.reports, "OK"), total)
		fmt.Fprintf(&b, "  %-18s %d / %d\n", "CATALOG PARTIAL:", countCatalog(data.reports, "PARTIAL"), total)
		fmt.Fprintf(&b, "  %-18s %d / %d\n", "CATALOG MISSING:", countCatalog(data.reports, "MISSING"), total)
		fmt.Fprintf(&b, "  %-18s %d / %d\n", "Fully done:", countReady(data.reports), total)
	}

	b.WriteString("\n=== Validation issues detail ===\n")
	issues := 0
	for _, r := range data.reports {
		if len(r.Reasons) == 0 {
			continue
		}
		issues++
		fmt.Fprintf(&b, "  %s:\n", r.Package)
		for _, reason := range r.Reasons {
			fmt.Fprintf(&b, "    - %s\n", reason)
		}
	}
	if issues == 0 {
		b.WriteString("  (none)\n")
	}

	if data.options.CatalogImage != "" {
		renderActionSummary(&b, data.reports)
		renderMissingVersions(&b, data.reports)
	}
	renderCSVLists(&b, data)

	b.WriteString("\n=== Generated files ===\n")
	if data.options.ValidateOnly {
		generatedFile(&b, data.options.OutputDir, "plcc-dump.json", "Filtered PLCC data")
	} else if data.hasFBC {
		generatedFile(&b, data.options.OutputDir, "fbc-output.yaml", "FBC blobs")
	}
	generatedFile(&b, data.options.OutputDir, "validation.jsonl", "Validation results")
	generatedFile(&b, data.options.OutputDir, "slog.json", "Operational log")
	if data.options.CatalogImage != "" {
		generatedFile(&b, data.options.OutputDir, "catalog-packages.txt", "Catalog package list")
		generatedFile(&b, data.options.OutputDir, "classification.json", "Classification report")
	}
	generatedFile(&b, data.options.OutputDir, "summary.txt", "Summary")
	return b.String()
}

func generatedFile(b *strings.Builder, dir, name, description string) {
	fmt.Fprintf(b, "  %-24s %s\n", filepath.Join(dir, name), description)
}

func countPLCC(reports []classify.OperatorReport, status classify.PLCCStatus) int {
	count := 0
	for _, r := range reports {
		if r.PLCC == status {
			count++
		}
	}
	return count
}

func countCatalog(reports []classify.OperatorReport, status string) int {
	count := 0
	for _, r := range reports {
		switch status {
		case "OK":
			if r.CatalogStatus == classify.CatalogStatusOK {
				count++
			}
		case "MISSING":
			if r.CatalogStatus == classify.CatalogStatusMissing {
				count++
			}
		case "PARTIAL":
			if r.CatalogStatus != classify.CatalogStatusOK && r.CatalogStatus != classify.CatalogStatusMissing {
				count++
			}
		}
	}
	return count
}

func countReady(reports []classify.OperatorReport) int {
	count := 0
	for _, r := range reports {
		if r.PLCC == classify.PLCCStatusOK && r.CatalogStatus == classify.CatalogStatusOK {
			count++
		}
	}
	return count
}

func countAction(reports []classify.OperatorReport, action classify.Action) int {
	count := 0
	for _, r := range reports {
		if r.PrimaryAction == action {
			count++
		}
	}
	return count
}

func actionVersions(r classify.OperatorReport, action classify.Action) []string {
	var versions []string
	for _, gap := range r.Gaps {
		if gap.Action == action {
			versions = append(versions, gap.Version)
		}
	}
	return versions
}

func renderActionSummary(b *strings.Builder, reports []classify.OperatorReport) {
	b.WriteString("\n=== Action classification ===\n")
	for _, action := range []classify.Action{
		classify.ActionFixPLCC, classify.ActionPLCCMissing, classify.ActionNoCatalogBundles,
		classify.ActionNeedsRebuild, classify.ActionOK,
	} {
		fmt.Fprintf(b, "  %-22s %d / %d\n", string(action)+":", countAction(reports, action), len(reports))
	}
	for _, action := range []classify.Action{classify.ActionFixPLCC, classify.ActionPLCCMissing, classify.ActionNeedsRebuild} {
		var lines []string
		var reasons []string
		for _, r := range reports {
			versions := actionVersions(r, action)
			if len(versions) == 0 && r.PrimaryAction != action {
				continue
			}
			if len(versions) > 0 {
				lines = append(lines, fmt.Sprintf("  %s: %s", r.Package, strings.Join(versions, ", ")))
			} else if action == classify.ActionPLCCMissing {
				lines = append(lines, fmt.Sprintf("  %s: package absent from PLCC", r.Package))
			} else {
				lines = append(lines, fmt.Sprintf("  %s: package-level issue", r.Package))
			}
			if action == classify.ActionFixPLCC {
				for _, reason := range r.Reasons {
					reasons = append(reasons, fmt.Sprintf("    %s: %s", r.Package, reason))
				}
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(b, "\n  --- %s ---\n", action)
		for _, line := range lines {
			fmt.Fprintln(b, line)
		}
		for _, line := range reasons {
			fmt.Fprintln(b, line)
		}
	}
}

func renderMissingVersions(b *strings.Builder, reports []classify.OperatorReport) {
	b.WriteString("\n=== Missing lifecycle versions ===\n")
	count := 0
	for _, r := range reports {
		if r.CatalogStatus == classify.CatalogStatusOK || r.CatalogStatus == classify.CatalogStatusMissing {
			continue
		}
		var versions []string
		for _, gap := range r.Gaps {
			versions = append(versions, gap.Version)
		}
		if len(versions) > 0 {
			fmt.Fprintf(b, "  %s: %s\n", r.Package, strings.Join(versions, ","))
			count++
		}
	}
	if count == 0 {
		b.WriteString("  (none)\n")
	}
}

func renderCSVLists(b *strings.Builder, data runData) {
	b.WriteString("\n=== CSV operator lists ===\n")
	for _, entry := range []struct {
		label string
		match func(classify.OperatorReport) bool
	}{
		{"Missing", func(r classify.OperatorReport) bool { return r.PLCC == classify.PLCCStatusMissing }},
		{"Duplicated", func(r classify.OperatorReport) bool { return r.PLCC == classify.PLCCStatusDuplicate }},
		{"With issues", func(r classify.OperatorReport) bool { return r.PLCC == classify.PLCCStatusInvalid }},
		{"PLCC OK", func(r classify.OperatorReport) bool { return r.PLCC == classify.PLCCStatusOK }},
	} {
		fmt.Fprintf(b, "- %s:%s\n", entry.label, csvMatches(data, entry.match))
	}
	if data.options.CatalogImage != "" {
		for _, entry := range []struct {
			label string
			match func(classify.OperatorReport) bool
		}{
			{"Catalog OK", func(r classify.OperatorReport) bool { return r.CatalogStatus == classify.CatalogStatusOK }},
			{"Catalog partial", func(r classify.OperatorReport) bool {
				return r.CatalogStatus != classify.CatalogStatusOK && r.CatalogStatus != classify.CatalogStatusMissing
			}},
			{"Catalog missing", func(r classify.OperatorReport) bool { return r.CatalogStatus == classify.CatalogStatusMissing }},
			{"Fully done", func(r classify.OperatorReport) bool {
				return r.PLCC == classify.PLCCStatusOK && r.CatalogStatus == classify.CatalogStatusOK
			}},
		} {
			fmt.Fprintf(b, "- %s:%s\n", entry.label, csvMatches(data, entry.match))
		}
	}
}

func csvMatches(data runData, match func(classify.OperatorReport) bool) string {
	byName := make(map[string]classify.OperatorReport, len(data.reports))
	for _, r := range data.reports {
		byName[r.Package] = r
	}
	var names []string
	for _, name := range data.names {
		if match(byName[name]) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " " + strings.Join(names, ",")
}
