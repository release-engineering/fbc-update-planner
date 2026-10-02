//go:build e2e

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

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/release-engineering/fbc-update-planner/internal/plcccheck"
)

// runPlccCheck invokes the reporting binary built once by TestMain, with paths
// relative to the test/e2e working directory.
func runPlccCheck(t *testing.T, args ...string) (stdout, stderr []byte, exitCode int) {
	t.Helper()
	return runPlccCheckWithEnv(t, nil, args...)
}

func runPlccCheckWithEnv(t *testing.T, env []string, args ...string) (stdout, stderr []byte, exitCode int) {
	t.Helper()

	timeout := 60 * time.Second
	if dl, ok := t.Deadline(); ok {
		timeout = time.Until(dl) - time.Second
		if timeout <= 0 {
			t.Fatalf("test deadline already passed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, plccCheckBinaryPath, args...)
	// --webhook derives the workflow link from the standard GitHub
	// Actions environment. Supplying stable values also keeps payload tests
	// independent of the environment that runs them.
	cmd.Env = append(os.Environ(),
		"GITHUB_SERVER_URL=https://github.example.test",
		"GITHUB_REPOSITORY=release-engineering/fbc-update-planner",
		"GITHUB_RUN_ID=12345",
	)
	cmd.Env = append(cmd.Env, env...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("plcc-check timed out after %v\nstdout: %s\nstderr: %s", timeout, outBuf.Bytes(), errBuf.Bytes())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return outBuf.Bytes(), errBuf.Bytes(), exitErr.ExitCode()
		}
		t.Fatalf("running plcc-check: %v\nstdout: %s\nstderr: %s", err, outBuf.Bytes(), errBuf.Bytes())
	}
	return outBuf.Bytes(), errBuf.Bytes(), 0
}

// slogField reads one field from a slog JSON line, failing the test if the
// line isn't valid JSON or the field is absent.
func slogField(t *testing.T, line string, field string) any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("slog line is not valid JSON: %s: %v", line, err)
	}
	v, ok := entry[field]
	if !ok {
		t.Fatalf("slog line missing field %q: %s", field, line)
	}
	return v
}

func assertFilesEqual(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("reading %s: %v", gotPath, err)
	}
	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match %s (got %d bytes, want %d bytes)",
			gotPath, wantPath, len(got), len(want))
	}
}

// TestPlccCheckOperatorsFile runs plcc-check against a small, fixed
// operators file (one passing, one failing, one missing, and one duplicated
// package) so the generated files can be compared against small, reviewable
// golden fixtures.
func TestPlccCheckOperatorsFile(t *testing.T) {
	outDir := t.TempDir()
	stdout, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/plcc.json",
		"-o", outDir,
		"testdata/plcc-check-operators.txt",
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	summary, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatalf("reading summary.txt: %v", err)
	}
	if !bytes.Equal(stdout, summary) {
		t.Errorf("stdout and summary.txt differ:\nstdout:\n%s\nsummary.txt:\n%s", stdout, summary)
	}
	assertFilesEqual(t, filepath.Join(outDir, "summary.txt"), "testdata/plcc-check/operators-summary.txt")

	assertFilesEqual(t,
		filepath.Join(outDir, "validation.jsonl"),
		"testdata/plcc-check/operators-validation.jsonl",
	)

	gotFBC, err := os.ReadFile(filepath.Join(outDir, "fbc-output.yaml"))
	if err != nil {
		t.Fatalf("reading fbc-output.yaml: %v", err)
	}
	refByPackage := splitYAMLReference(t, "testdata/reference-fbc-validated.yaml")
	const targetPkg = "aws-efs-csi-driver-operator"
	want, ok := refByPackage[targetPkg]
	if !ok {
		t.Fatalf("package %s not found in reference file", targetPkg)
	}
	if string(gotFBC) != want {
		t.Errorf("fbc-output.yaml does not match reference segment for %s (got %d bytes, want %d bytes)",
			targetPkg, len(gotFBC), len(want))
	}

	slogData, err := os.ReadFile(filepath.Join(outDir, "slog.json"))
	if err != nil {
		t.Fatalf("reading slog.json: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(slogData)), "\n")
	var gotMsgs []string
	for _, line := range lines {
		gotMsgs = append(gotMsgs, slogField(t, line, "msg").(string))
	}
	wantMsgs := []string{"assessment starting", "loaded PLCC snapshot", "assessment complete"}
	if strings.Join(gotMsgs, ",") != strings.Join(wantMsgs, ",") {
		t.Errorf("slog messages = %v, want %v", gotMsgs, wantMsgs)
	}
	if got := slogField(t, lines[len(lines)-1], "operators"); got != float64(4) {
		t.Errorf("logged operator count = %v, want 4", got)
	}
}

// TestPlccCheckCatalogPresence runs plcc-check with --catalog-image
// pointed at a local FBC directory (no registry/network needed; opm render
// works the same against a local directory as against a remote image) to
// exercise the catalog-membership check. testdata/catalog-fbc contains
// aws-efs-csi-driver-operator only, so it hits both a catalog-present and a
// catalog-absent operator from the shared 4-operator fixture.
func TestPlccCheckCatalogPresence(t *testing.T) {
	outDir := t.TempDir()
	_, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/plcc.json",
		"-o", outDir,
		"--catalog-image", "testdata/catalog-fbc",
		"testdata/plcc-check-operators.txt",
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	assertFilesEqual(t, filepath.Join(outDir, "summary.txt"), "testdata/plcc-check/catalog-summary.txt")

	catalogPackages, err := os.ReadFile(filepath.Join(outDir, "catalog-packages.txt"))
	if err != nil {
		t.Fatalf("reading catalog-packages.txt: %v", err)
	}
	if string(catalogPackages) != "aws-efs-csi-driver-operator\n" {
		t.Errorf("catalog-packages.txt = %q, want %q", catalogPackages, "aws-efs-csi-driver-operator\n")
	}
}

// TestPlccCheckCatalogVersionCoverage checks statuses, counts, actions, and
// per-version findings from a real opm render of the local catalog fixture.
func TestPlccCheckCatalogVersionCoverage(t *testing.T) {
	fixtureDir := t.TempDir()
	operatorsPath := filepath.Join(fixtureDir, "operators.txt")
	if err := os.WriteFile(operatorsPath, []byte("operator-full\noperator-partial\noperator-missing\naws-efs-csi-driver-operator\ncli-manager\n"), 0o600); err != nil {
		t.Fatalf("writing operators fixture: %v", err)
	}

	outDir := t.TempDir()
	stdout, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/plcc.json",
		"-o", outDir,
		"--catalog-image", "testdata/catalog-fbc-versions",
		operatorsPath,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	report := readAssessment(t, outDir)
	wantCounts := plcccheck.CatalogCounts{OK: 1, Incomplete: 2, Missing: 1, NoBundles: 1}
	if report.Summary.Catalog == nil || *report.Summary.Catalog != wantCounts || report.Summary.FullyOK != 0 {
		t.Fatalf("unexpected catalog counts: %+v", report.Summary)
	}
	for _, tt := range []struct {
		name, coverage string
		action         plcccheck.Action
	}{
		{"operator-full", "OK", plcccheck.AddPLCC},
		{"operator-partial", "2/3", plcccheck.AddPLCC},
		{"operator-missing", "MISSING", plcccheck.AddPLCC},
		{"aws-efs-csi-driver-operator", "NO BUNDLES", plcccheck.FixPLCC},
		{"cli-manager", "1/2", plcccheck.FixPLCC},
	} {
		pkg := assessedPackage(t, report, tt.name)
		if pkg.Catalog.Status() != tt.coverage || pkg.Action != tt.action {
			t.Errorf("%s: got %s / %s, want %s / %s", tt.name, pkg.Catalog.Status(), pkg.Action, tt.coverage, tt.action)
		}
	}
	for _, tt := range []struct{ name, version string }{
		{"operator-partial", "1.2"}, {"cli-manager", "0.2"}, {"operator-missing", "1.0"},
	} {
		pkg := assessedPackage(t, report, tt.name)
		found := false
		for _, issue := range pkg.Issues {
			if issue.Kind == plcccheck.MissingCatalogLifecycleVersion && issue.Version != nil && issue.Version.String() == tt.version {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing lifecycle finding for %s: %+v", tt.name, tt.version, pkg.Issues)
		}
	}
	if !strings.Contains(string(stdout), "[catalog-lifecycle-version-missing]") {
		t.Error("text report missing labeled catalog findings")
	}
}

// Lifecycle entries with empty names are ignored during catalog ingestion.
func TestPlccCheckCatalogEmptyPackageName(t *testing.T) {
	outDir := t.TempDir()
	_, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/plcc.json",
		"-o", outDir,
		"--catalog-image", "testdata/catalog-fbc-empty-package",
		"testdata/plcc-check-operators.txt",
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
}

// TestPlccCheckCatalogNullPackageName verifies that a rendered lifecycle
// object with a null package is ignored rather than becoming a stale catalog
// package named "null" in an all-packages report.
func TestPlccCheckCatalogNullPackageName(t *testing.T) {
	fakeBin := t.TempDir()
	fakeOpm := filepath.Join(fakeBin, "opm")
	if err := os.WriteFile(fakeOpm, []byte(`#!/bin/sh
printf '%s\n' '{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":null}'
`), 0o755); err != nil {
		t.Fatalf("writing fake opm: %v", err)
	}

	outDir := t.TempDir()
	stdout, stderr, exitCode := runPlccCheckWithEnv(t,
		[]string{"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"-i", "testdata/untranslatable.json",
		"--validators", "none",
		"-o", outDir,
		"--catalog-image", "unused-catalog-reference",
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	if strings.Contains(string(stdout), "null") {
		t.Errorf("stdout contains bogus null package:\n%s", stdout)
	}
	if !strings.Contains(string(stdout), "Total operators: 1") {
		t.Errorf("stdout has unexpected operator count:\n%s", stdout)
	}
	catalogPackages, err := os.ReadFile(filepath.Join(outDir, "catalog-packages.txt"))
	if err != nil {
		t.Fatalf("reading catalog-packages.txt: %v", err)
	}
	if len(catalogPackages) != 0 {
		t.Errorf("catalog-packages.txt = %q, want empty", catalogPackages)
	}
}

// TestPlccCheckWebhook checks each section selection and the payload link.
func TestPlccCheckWebhook(t *testing.T) {
	const runURL = "https://github.example.test/release-engineering/fbc-update-planner/actions/runs/12345"
	for _, tc := range []struct {
		name, sections                                         string
		catalog, wantSummary, wantTable, wantList, wantDetails bool
	}{
		{name: "table only", sections: "table", wantTable: true},
		{name: "list only", sections: "list", wantList: true},
		{name: "summary and table", sections: "summary,table", catalog: true, wantSummary: true, wantTable: true},
		{name: "summary and list", sections: "summary,list", catalog: true, wantSummary: true, wantList: true},
		{name: "summary only", sections: "summary", catalog: true, wantSummary: true},
		{name: "details only", sections: "details", catalog: true, wantDetails: true},
		{name: "daily report", sections: "summary,table,list,details", catalog: true, wantSummary: true, wantTable: true, wantList: true, wantDetails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			args := []string{"--webhook", tc.sections, "--validators", "syntax,catalog", "-i", "testdata/plcc.json", "-o", outDir}
			if tc.catalog {
				args = append(args, "--catalog-image", "testdata/catalog-fbc")
			}
			args = append(args, "testdata/plcc-check-operators.txt")
			_, stderr, code := runPlccCheck(t, args...)
			if code != 0 {
				t.Fatalf("exit code %d; stderr: %s", code, stderr)
			}
			data, err := os.ReadFile(filepath.Join(outDir, "slack-payload.json"))
			if err != nil {
				t.Fatal(err)
			}
			var payload plcccheck.SlackPayload
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Text != "Operator lifecycle assessment. "+runURL {
				t.Errorf("unexpected fallback text: %q", payload.Text)
			}
			var summary, operators, details, scope, footer, legend string
			var section string
			var headings []string
			lists := make(map[string]string)
			var listHeading string
			for _, block := range payload.Blocks {
				if block.Type == "header" {
					if block.Text == nil || block.Text.Type != "plain_text" {
						t.Fatalf("invalid Slack header: %+v", block)
					}
					if strings.HasPrefix(block.Text.Text, "READY operators:") {
						if section != "Summary" {
							t.Fatal("READY heading is outside Summary")
						}
						headings = append(headings, block.Text.Text)
						summary += block.Text.Text + "\n"
						continue
					}
					section = block.Text.Text
					headings = append(headings, section)
					continue
				}
				if block.Type == "rich_text" {
					if block.Text != nil || len(block.Elements) != 1 || block.Elements[0].Type != "rich_text_preformatted" {
						t.Fatalf("operator table/list is not preformatted: %+v", block)
					}
					leaves := block.Elements[0].Elements
					if len(leaves) != 1 || leaves[0].Type != "text" {
						t.Fatalf("table does not contain literal text: %+v", leaves)
					}
					if listHeading != "" {
						lists[listHeading] += leaves[0].Text
						listHeading = ""
					} else {
						operators += strings.Join(strings.Fields(leaves[0].Text), " ")
					}
					continue
				}
				if block.Text == nil {
					t.Fatalf("missing block text: %+v", block)
				}
				value := block.Text.Text
				switch {
				case strings.Contains(value, "Open workflow run"):
					footer += value
				case section == "Summary":
					summary += value + "\n"
				case strings.HasPrefix(value, "Details:"):
					details += value
				case strings.HasPrefix(value, "Scope\n"):
					scope += value
				case section == "Table":
					legend += value
				default:
					for _, action := range []string{"✅ OK", "📋 PLCCDATA", "📦 OPERATOR", "➖ SKIPPED"} {
						if strings.HasPrefix(value, action) {
							if strings.HasSuffix(value, "\nNone") {
								lists[action] = ""
							} else {
								listHeading = action
							}
						}
					}
				}
			}
			wantHeadings := []string{"Operator lifecycle assessment"}
			for _, item := range []struct {
				name    string
				enabled bool
			}{
				{"Summary", tc.wantSummary}, {"READY operators: 0/4", tc.wantSummary && tc.catalog},
				{"Table", tc.wantTable}, {"List", tc.wantList}, {"Details", tc.wantDetails},
			} {
				if item.enabled {
					wantHeadings = append(wantHeadings, item.name)
				}
			}
			if strings.Join(headings, ",") != strings.Join(wantHeadings, ",") {
				t.Errorf("section headers = %v, want %v", headings, wantHeadings)
			}
			if (summary != "") != tc.wantSummary || (operators != "") != tc.wantTable || (len(lists) != 0) != tc.wantList || (details != "") != tc.wantDetails {
				t.Errorf("unexpected sections: summary=%t table=%t list=%t details=%t", summary != "", operators != "", len(lists) != 0, details != "")
			}
			if strings.Contains(summary, "Catalog X/Y") || (legend != "") != (tc.wantTable && tc.catalog) {
				t.Errorf("misplaced coverage legend: summary=%q legend=%q", summary, legend)
			}
			if tc.wantTable && tc.catalog && legend != "PLCC/Catalog X/Y: X versions available, Y versions required." {
				t.Errorf("unexpected coverage legend: %q", legend)
			}
			if !strings.Contains(scope, "Selected operators (plcc-check-operators.txt)") || !strings.Contains(footer, runURL) {
				t.Errorf("missing scope or artifact link: %s / %s", scope, footer)
			}
			if tc.wantSummary && !strings.Contains(summary, "Total operators: 4") {
				t.Errorf("missing operator count: %s", summary)
			}
			if tc.wantSummary {
				prefix := "Total operators: 4\nSkipped operators: 0    (Status counts exclude skipped operators)\n"
				if tc.catalog {
					prefix += "READY operators: 0/4\n"
				}
				if !strings.Contains(summary, prefix+"PLCC:") || strings.Contains(summary, "Non-skipped operators:") || strings.Contains(summary, "Fully OK") {
					t.Errorf("incorrect summary order or labels: %s", summary)
				}
				want := "PLCC: OK 2 | MISSING 1 | INCOMPLETE 0 | INVALID 0 | REGRESSED 0 | DUPLICATE 1\nCatalog: NOT CHECKED"
				if tc.catalog {
					want = "PLCC: OK 1 | MISSING 1 | INCOMPLETE 0 | INVALID 0 | REGRESSED 1 | DUPLICATE 1\nCatalog: OK 0 | MISSING 0 | INCOMPLETE 0 | NO BUNDLES 4"
				}
				if !strings.Contains(summary, want) {
					t.Errorf("unexpected summary counts or ordering: %s", summary)
				}
			}
			if tc.wantSummary && strings.Contains(summary, "Catalog image: testdata/catalog-fbc\n\nTotal operators:") != tc.catalog {
				t.Errorf("incorrect catalog source in Slack summary: %s", summary)
			}
			if tc.wantTable {
				if !strings.Contains(operators, "ACTION | OPERATOR | PLCC | CATALOG | SKIPPED") {
					t.Errorf("missing table header: %s", operators)
				}
				want := "✅ | aws-efs-csi-driver-operator | ✅ | NOT CHECKED"
				if tc.catalog {
					want = "📋 PLCCDATA | aws-efs-csi-driver-operator | REGRESSED | NO BUNDLES"
				}
				if !strings.Contains(operators, want) || !strings.Contains(operators, "📋 PLCCDATA | totally-nonexistent-operator-xyz | MISSING") {
					t.Errorf("missing action rows: %s", operators)
				}
			}
			if tc.wantList {
				wantOK := "aws-efs-csi-driver-operator,barbican-operator"
				wantPLCC := "totally-nonexistent-operator-xyz,amq-streams"
				wantOperator := ""
				if tc.catalog {
					wantPLCC = "aws-efs-csi-driver-operator," + wantPLCC
					wantOK = ""
					wantOperator = "barbican-operator"
				}
				if len(lists) != 4 || lists["✅ OK"] != wantOK || lists["📋 PLCCDATA"] != wantPLCC || lists["📦 OPERATOR"] != wantOperator || lists["➖ SKIPPED"] != "" {
					t.Errorf("incorrect action lists: %v", lists)
				}
			}
			if tc.wantDetails && (!strings.Contains(details, "[plcc-package-missing]") || !strings.Contains(details, "[catalog-bundles-missing]")) {
				t.Errorf("missing labeled details: %s", details)
			}
		})
	}
}

func TestPlccCheckWebhookRejectsUnknownSection(t *testing.T) {
	_, stderr, code := runPlccCheck(t, "--webhook", "summary,unknown")
	if code != 1 || !strings.Contains(string(stderr), `unsupported webhook section "unknown"`) {
		t.Fatalf("exit code %d; stderr: %s", code, stderr)
	}
}

func TestPlccCheckScopesCommaSeparatedValidationResult(t *testing.T) {
	fixtureDir := t.TempDir()
	inputPath := filepath.Join(fixtureDir, "plcc.json")
	operatorsPath := filepath.Join(fixtureDir, "operators.txt")
	const input = `{"data":[{"name":"Combined","package":"a,b","is_operator":true,"versions":[{"name":"bad","phases":[]}]}]}`
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatalf("writing PLCC fixture: %v", err)
	}
	// Repeating a also verifies requested names are treated as a set: totals
	// must use the same cardinality as deduplicated validation buckets.
	if err := os.WriteFile(operatorsPath, []byte("a\na\n"), 0o600); err != nil {
		t.Fatalf("writing operators fixture: %v", err)
	}

	outDir := t.TempDir()
	_, stderr, exitCode := runPlccCheck(t,
		"-i", inputPath,
		"-o", outDir,
		operatorsPath,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	report := readAssessment(t, outDir)
	if report.Summary.Total != 1 || report.Summary.PLCC.Invalid != 1 || report.Summary.PLCC.OK != 0 || len(report.Assessment.Packages) != 1 || report.Assessment.Packages[0].Name != "a" {
		t.Fatalf("unexpected selection: %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "validation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var result struct {
			PackageName string `json:"packageName"`
		}
		if err := json.Unmarshal([]byte(line), &result); err != nil || result.PackageName != "a" {
			t.Fatalf("validation result escaped selected alias: %s (%v)", line, err)
		}
	}
}

func TestPlccCheckSurfacesStaleCatalogPackage(t *testing.T) {
	outDir := t.TempDir()
	stdout, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/untranslatable.json",
		"--validators", "none",
		"--catalog-image", "testdata/catalog-fbc-stale",
		"-o", outDir,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	report := readAssessment(t, outDir)
	pkg := assessedPackage(t, report, "stale-operator")
	if report.Summary.Total != 2 || report.Summary.PLCC.Missing != 1 || report.Summary.PLCC.Invalid != 1 || report.Summary.Catalog.NoBundles != 2 {
		t.Fatalf("unexpected counts: %+v", report.Summary)
	}
	if pkg.Action != plcccheck.AddPLCC || pkg.PLCC != plcccheck.PLCCAbsent || pkg.Catalog.Status() != "NO BUNDLES" {
		t.Fatalf("unexpected stale package: %+v", pkg)
	}
	if !strings.Contains(string(stdout), "[plcc-version-regressed]") {
		t.Error("missing regression finding for stale catalog lifecycle")
	}
}

// TestPlccCheckAllPackages runs plcc-check with no operators file (the
// "check everything in PLCC" mode) and no validators, so the resulting FBC
// output can be compared byte-for-byte against the existing e2e reference.
func TestPlccCheckAllPackages(t *testing.T) {
	outDir := t.TempDir()
	_, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/plcc.json",
		"--validators", "none",
		"-o", outDir,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	gotFBC, err := os.ReadFile(filepath.Join(outDir, "fbc-output.yaml"))
	if err != nil {
		t.Fatalf("reading fbc-output.yaml: %v", err)
	}
	wantFBC, err := os.ReadFile("testdata/reference-fbc.yaml")
	if err != nil {
		t.Fatalf("reading reference-fbc.yaml: %v", err)
	}
	if string(gotFBC) != string(wantFBC) {
		t.Errorf("fbc-output.yaml does not match testdata/reference-fbc.yaml (got %d bytes, want %d bytes)",
			len(gotFBC), len(wantFBC))
	}

	report := readAssessment(t, outDir)
	if report.Summary.Total != 142 || report.Summary.PLCC.OK != 61 || report.Summary.PLCC.Invalid != 81 || report.Summary.PLCC.Missing != 0 {
		t.Fatalf("unexpected all-package counts: %+v", report.Summary)
	}
}

func readAssessment(t *testing.T, dir string) plcccheck.Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "assessment.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report plcccheck.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func assessedPackage(t *testing.T, report plcccheck.Report, name string) plcccheck.PackageAssessment {
	t.Helper()
	for _, pkg := range report.Assessment.Packages {
		if pkg.Name == name {
			return pkg
		}
	}
	t.Fatalf("package %q missing from assessment", name)
	return plcccheck.PackageAssessment{}
}
