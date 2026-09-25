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
)

const plccCheckScript = "../../scripts/plcc-check.sh"

// runPlccCheck runs scripts/plcc-check.sh with args, relative to the test/e2e
// working directory (matching how runBinary resolves "testdata/..." paths).
// The script builds its own copy of plcc2fbc via "make build", so it needs a
// longer timeout than runBinary's.
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

	cmd := exec.CommandContext(ctx, "bash", append([]string{plccCheckScript}, args...)...)
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
			t.Fatalf("script timed out after %v\nstdout: %s\nstderr: %s", timeout, outBuf.Bytes(), errBuf.Bytes())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return outBuf.Bytes(), errBuf.Bytes(), exitErr.ExitCode()
		}
		t.Fatalf("running script: %v\nstdout: %s\nstderr: %s", err, outBuf.Bytes(), errBuf.Bytes())
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

// TestPlccCheckOperatorsFile runs plcc-check.sh against a small, fixed
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
	// The "Generated files" section embeds outDir's absolute path, which
	// differs on every run; normalize it before comparing to the golden file.
	gotSummary := strings.ReplaceAll(string(summary), outDir, "$OUTDIR")
	wantSummary, err := os.ReadFile("testdata/plcc-check/operators-summary.txt")
	if err != nil {
		t.Fatalf("reading golden summary: %v", err)
	}
	if gotSummary != string(wantSummary) {
		t.Errorf("summary.txt mismatch:\ngot:\n%s\nwant:\n%s", gotSummary, wantSummary)
	}

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
	wantMsgs := []string{
		"plcc2fbc starting",
		"resolved validators",
		"fetched products from PLCC",
		"requested package not found in PLCC data",
		"filtered products",
		"PLCC catalog validation",
		"PLCC product validation",
		"PLCC product expansion",
		"wrote FBC data",
	}
	if strings.Join(gotMsgs, ",") != strings.Join(wantMsgs, ",") {
		t.Errorf("unexpected slog message sequence:\ngot:  %v\nwant: %v", gotMsgs, wantMsgs)
	}

	for i, line := range lines {
		var want map[string]any
		switch gotMsgs[i] {
		case "filtered products":
			want = map[string]any{"count": float64(4)}
		case "PLCC product validation":
			want = map[string]any{"passed": float64(1), "filtered": float64(1)}
		case "PLCC product expansion":
			want = map[string]any{"count": float64(1)}
		case "wrote FBC data":
			want = map[string]any{"count": float64(1)}
		default:
			continue
		}
		for field, wantVal := range want {
			if got := slogField(t, line, field); got != wantVal {
				t.Errorf("slog line %q: field %q = %v, want %v", gotMsgs[i], field, got, wantVal)
			}
		}
	}
}

// TestPlccCheckCatalogPresence runs plcc-check.sh with --catalog-image
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

	summary, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatalf("reading summary.txt: %v", err)
	}
	gotSummary := strings.ReplaceAll(string(summary), outDir, "$OUTDIR")
	gotSummary = strings.ReplaceAll(gotSummary, "testdata/catalog-fbc", "$CATALOG_IMAGE")
	wantSummary, err := os.ReadFile("testdata/plcc-check/catalog-summary.txt")
	if err != nil {
		t.Fatalf("reading golden summary: %v", err)
	}
	if gotSummary != string(wantSummary) {
		t.Errorf("summary.txt mismatch:\ngot:\n%s\nwant:\n%s", gotSummary, wantSummary)
	}

	catalogPackages, err := os.ReadFile(filepath.Join(outDir, "catalog-packages.txt"))
	if err != nil {
		t.Fatalf("reading catalog-packages.txt: %v", err)
	}
	if string(catalogPackages) != "aws-efs-csi-driver-operator\n" {
		t.Errorf("catalog-packages.txt = %q, want %q", catalogPackages, "aws-efs-csi-driver-operator\n")
	}
}

// TestPlccCheckCatalogVersionCoverage verifies that --catalog-image checks
// per-version coverage of shipped bundle versions against lifecycle entries.
// The fixture directory testdata/catalog-fbc-versions provides five operators
// covering each catalog status: full bundle coverage (OK), partial coverage
// (X/Y), no lifecycle entry (MISSING), lifecycle-only with no bundles (OK),
// and a PLCC-valid operator with partial catalog coverage (exercises the
// done-marker guard). Synthetic operator names (operator-full, operator-partial,
// operator-missing) are intentionally absent from testdata/plcc.json so their
// PLCC status is MISSING, isolating the catalog coverage logic under test.
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
	for _, want := range []string{
		"OK         operator-full",
		"2/3        operator-partial",
		"MISSING    operator-missing",
		"OK         aws-efs-csi-driver-operator",
		"1/2        cli-manager",
		"CATALOG OK:        2 / 5",
		"CATALOG PARTIAL:   2 / 5",
		"CATALOG MISSING:   1 / 5",
		"Fully done:        1 / 5",
	} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	// aws-efs-csi-driver-operator is PLCC OK + catalog OK → earns the done marker
	if !strings.Contains(string(stdout), "*  OK         OK         aws-efs-csi-driver-operator") {
		t.Errorf("stdout missing done marker for aws-efs-csi-driver-operator:\n%s", stdout)
	}
	// operator-partial is PLCC MISSING + catalog 2/3 → no done marker
	if !strings.Contains(string(stdout), "     MISSING    2/3") {
		t.Errorf("stdout missing partial operator row without done marker:\n%s", stdout)
	}
	// cli-manager is PLCC OK + catalog 1/2 → no done marker (exercises the
	// catalog-partial guard on a PLCC-valid operator)
	if !strings.Contains(string(stdout), "     OK         1/2") {
		t.Errorf("stdout missing cli-manager partial row without done marker:\n%s", stdout)
	}
	if strings.Contains(string(stdout), "*  OK         1/2") {
		t.Errorf("PLCC OK + catalog partial operator should not earn the done marker:\n%s", stdout)
	}
	// Missing lifecycle versions section
	if !strings.Contains(string(stdout), "=== Missing lifecycle versions ===") {
		t.Errorf("stdout missing 'Missing lifecycle versions' section:\n%s", stdout)
	}
	for _, want := range []string{
		"  cli-manager: 0.2",
		"  operator-partial: 1.2",
	} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("stdout missing missing-lifecycle entry %q:\n%s", want, stdout)
		}
	}
	// operator-missing has MISSING status → should not appear in missing lifecycle section
	if strings.Contains(string(stdout), "  operator-missing:") {
		t.Errorf("operator-missing should be skipped in missing lifecycle versions (no lifecycle data):\n%s", stdout)
	}
	// Catalog partial CSV list
	if !strings.Contains(string(stdout), "- Catalog partial: ") {
		t.Errorf("stdout missing catalog partial CSV entry:\n%s", stdout)
	}
}

// TestPlccCheckCatalogEmptyPackageName is a regression test for a bash
// pitfall in fetch_catalog_packages: its final statement used to be a bare
// "[[ -n "$name" ]] && arr+=(...)" with nothing after it. Under "set -e",
// if that guard evaluates false on the last line read, the function's exit
// status becomes non-zero and the whole script dies silently. That trigger
// requires an empty catalog package name to sort last, which happens when a
// catalog schema entry has an empty (but present) "package" field and it's
// the only entry opm renders; testdata/catalog-fbc-empty-package reproduces
// that. This only checks the script survives, not its output content.
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
	if !strings.Contains(string(stdout), "Total operators:   1") {
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

// TestPlccCheckWebhook verifies that the script, rather than the workflow,
// constructs the requested Slack payload sections from its collected results.
func TestPlccCheckWebhook(t *testing.T) {
	const runURL = "https://github.example.test/release-engineering/fbc-update-planner/actions/runs/12345"

	for _, tc := range []struct {
		name        string
		sections    string
		catalog     bool
		wantSummary bool
		wantList    bool
	}{
		{name: "list only", sections: "list", wantList: true},
		{name: "summary and list with catalog", sections: "summary,list", catalog: true, wantSummary: true, wantList: true},
		{name: "summary only with catalog", sections: "summary", catalog: true, wantSummary: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			args := []string{
				"--webhook", tc.sections,
				"--validators", "syntax,catalog",
				"-i", "testdata/plcc.json",
				"-o", outDir,
			}
			if tc.catalog {
				args = append(args, "--catalog-image", "testdata/catalog-fbc")
			}
			args = append(args, "testdata/plcc-check-operators.txt")
			_, stderr, exitCode := runPlccCheck(t, args...)
			if exitCode != 0 {
				t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
			}

			data, err := os.ReadFile(filepath.Join(outDir, "slack-payload.json"))
			if err != nil {
				t.Fatalf("reading slack payload: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("payload is not valid JSON: %v", err)
			}
			title := "Operator lifecycle assessment (plcc-check-operators.txt)"
			if got, want := payload["text"], title+". "+runURL; got != want {
				t.Errorf("payload text = %q, want %q", got, want)
			}

			blocks, ok := payload["blocks"].([]any)
			if !ok {
				t.Fatalf("payload blocks = %#v, want array", payload["blocks"])
			}
			var summary, operators string
			for _, block := range blocks {
				candidate, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if candidate["type"] == "section" {
					if text, ok := candidate["text"].(map[string]any); ok {
						if value, ok := text["text"].(string); ok {
							if strings.HasPrefix(value, "*Summary*\n") {
								summary = value
							}
							if strings.Contains(value, "aws-efs-csi-driver-operator") && strings.Contains(value, "PLCC:") {
								operators += value
							}
						}
					}
				}
			}
			if got := summary != ""; got != tc.wantSummary {
				t.Errorf("payload has summary = %t, want %t", got, tc.wantSummary)
			}
			if got := operators != ""; got != tc.wantList {
				t.Errorf("payload has operator list = %t, want %t", got, tc.wantList)
			}
			if tc.wantSummary && (!strings.Contains(summary, "• Scope:") || !strings.Contains(summary, "• Operators assessed:")) {
				t.Errorf("summary is missing scope or assessed count: %q", summary)
			}
			if tc.catalog && tc.wantSummary && !strings.Contains(summary, "*Ready in PLCC and catalog:") {
				t.Errorf("summary is missing the highlighted ready result: %q", summary)
			}
			if tc.catalog && tc.wantSummary && !strings.Contains(summary, "Catalog partial:") {
				t.Errorf("summary is missing the catalog partial status: %q", summary)
			}
			if !tc.wantList {
				if tc.wantSummary && tc.catalog && !strings.Contains(string(data), "Operators ready in PLCC and catalog") {
					t.Error("summary-only catalog payload is missing the ready operator list")
				}
			}
			if !strings.Contains(string(data), "Open workflow run and download artifacts") {
				t.Error("payload is missing workflow artifact link")
			}
			if tc.wantList && (!strings.HasPrefix(operators, "```\n") || !strings.HasSuffix(operators, "\n```")) {
				t.Error("operator list must be a monospaced Markdown block")
			}
			if tc.wantList && strings.Contains(operators, " — ") {
				t.Error("operator list must not contain dash separators")
			}
			if tc.catalog && tc.wantList && !strings.Contains(operators, "PLCC: OK         Catalog: OK") {
				t.Errorf("operator list has unaligned status columns: %q", operators)
			}
			if tc.catalog && tc.wantList && !strings.Contains(operators, "✅  aws-efs-csi-driver-operator") {
				t.Errorf("operator list does not highlight fully successful operators: %q", operators)
			}
			if tc.catalog && tc.wantList && strings.Count(operators, "✅") != 1 {
				t.Errorf("operator list contains %d success markers, want 1", strings.Count(operators, "✅"))
			}
			if strings.Contains(string(data), "CSV operator lists") {
				t.Error("payload must not include CSV operator lists")
			}
			if strings.Contains(string(data), `"type": "table"`) {
				t.Error("payload must use Markdown instead of Block Kit tables")
			}
		})
	}
}

func TestPlccCheckWebhookRejectsUnknownSection(t *testing.T) {
	_, stderr, exitCode := runPlccCheck(t, "--webhook", "summary,details")
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", exitCode, stderr)
	}
	if !strings.Contains(string(stderr), "unsupported webhook section: details") {
		t.Errorf("stderr = %q, want unsupported-section error", stderr)
	}
}

func TestPlccCheckMissingFBCOutput(t *testing.T) {
	outDir := t.TempDir()
	stdout, stderr, exitCode := runPlccCheck(t,
		"-i", "testdata/untranslatable.json",
		"--validators", "none",
		"-o", outDir,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	if _, err := os.Stat(filepath.Join(outDir, "fbc-output.yaml")); !os.IsNotExist(err) {
		t.Errorf("fbc-output.yaml should not exist; stat error = %v", err)
	}
	if strings.Contains(string(stdout), filepath.Join(outDir, "fbc-output.yaml")+" FBC blobs") {
		t.Error("stdout claims the missing FBC output was generated")
	}
	if !strings.Contains(string(stderr), "not found") {
		t.Errorf("stderr = %q, want missing-file error", stderr)
	}
	summary, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatalf("reading summary.txt: %v", err)
	}
	if !bytes.Equal(stdout, summary) {
		t.Errorf("stdout and summary.txt differ:\nstdout:\n%s\nsummary.txt:\n%s", stdout, summary)
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
	stdout, stderr, exitCode := runPlccCheck(t,
		"-i", inputPath,
		"-o", outDir,
		operatorsPath,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}
	for _, want := range []string{
		"Total operators:   1",
		"PLCC INVALID:      1 / 1",
		"PLCC OK:           0 / 1",
	} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(string(stdout), "PLCC INVALID:      2 / 1") || strings.Contains(string(stdout), "PLCC OK:           -") {
		t.Errorf("summary contains inconsistent counts:\n%s", stdout)
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
	for _, want := range []string{
		"MISSING    OK         stale-operator",
		"Total operators:   2",
		"PLCC MISSING:      1 / 2",
		"PLCC INVALID:      1 / 2",
		"PLCC OK:           0 / 2",
		"CATALOG OK:        1 / 2",
		"CATALOG PARTIAL:   0 / 2",
		"CATALOG MISSING:   1 / 2",
	} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// TestPlccCheckAllPackages runs plcc-check.sh with no operators file (the
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

	summary, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatalf("reading summary.txt: %v", err)
	}
	for _, want := range []string{
		"Total operators:   142",
		"PLCC OK:           61 / 142",
		"PLCC INVALID:      81 / 142",
		"PLCC MISSING:      0 / 142",
	} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("summary.txt missing expected line %q; full summary:\n%s", want, summary)
		}
	}
}
