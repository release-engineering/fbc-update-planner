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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/release-engineering/fbc-update-planner/internal/plcccheck"
)

func TestPLCCCheckInterruptsFetch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to a child process is unsupported on Windows")
	}
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stall the HTTPS CONNECT request before any PLCC request reaches the API.
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer proxy.Close()
	defer close(release)
	dir := t.TempDir()
	payload := filepath.Join(dir, "slack-payload.json")
	if err := os.WriteFile(payload, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), plccCheckBinaryPath, "-o", dir)
	cmd.Env = append(os.Environ(), "HTTPS_PROXY="+proxy.URL, "NO_PROXY=", "no_proxy=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-started:
	case <-done:
		t.Fatalf("command exited before fetching: %s", stderr.String())
	case <-time.After(5 * time.Second):
		t.Fatal("command did not reach the local proxy")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("interrupt exit = %v, want status 1; stderr: %s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C did not cancel the stalled PLCC fetch")
	}
	if stdout.Len() != 0 || (!strings.Contains(stderr.String(), "context canceled") && !strings.Contains(stderr.String(), "interrupt signal received")) {
		t.Fatalf("cancellation output: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(payload); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted run left a stale Slack payload: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(dir, "slog.json"))
	if err != nil || !bytes.Contains(log, []byte("assessment failed")) {
		t.Fatalf("missing cancellation log: %s, %v", log, err)
	}
}

func TestPLCCCheckCommand(t *testing.T) {
	run := func(t *testing.T, args ...string) ([]byte, int) {
		t.Helper()
		stdout, stderr, code := runPlccCheck(t, args...)
		if code == 0 && len(stderr) != 0 {
			t.Fatalf("unexpected stderr: %s", stderr)
		}
		if code != 0 && len(stderr) == 0 {
			t.Fatal("error without diagnostic")
		}
		return stdout, code
	}
	t.Run("configured selection and skip groups", func(t *testing.T) {
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")
		t.Setenv("GITHUB_REPOSITORY", "org/repo")
		t.Setenv("GITHUB_RUN_ID", "42")
		dir, baseline := t.TempDir(), t.TempDir()
		config := filepath.Join(t.TempDir(), "report.yaml")
		selection := "selected: [full, mixed, validator, bundle-only, no-bundles, mixed]\n"
		if err := os.WriteFile(config, []byte(selection), 0644); err != nil {
			t.Fatal(err)
		}
		common := []string{"-i", "../../internal/plcccheck/testdata/plcc.json", "--catalog-input", "../../internal/plcccheck/testdata/catalog.json",
			"--validators", "syntax,catalog", "--config", config, "--webhook", "summary,table,list,details"}
		if _, code := run(t, append(common, "-o", baseline)...); code != 0 {
			t.Fatalf("baseline exited %d", code)
		}
		policy := "skipped:\n  - reason: Shared exception\n    operators: [mixed, validator, outside-selection]\n  - reason: No PLCC expected\n    operators: [bundle-only]\n"
		if err := os.WriteFile(config, []byte(selection+policy), 0644); err != nil {
			t.Fatal(err)
		}
		stdout, code := run(t, append(common, "-o", dir)...)
		if code != 0 {
			t.Fatalf("configured report exited %d", code)
		}
		summary, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
		if err != nil || !bytes.Equal(stdout, summary) {
			t.Fatalf("stdout differs from summary: %v", err)
		}
		if !bytes.Contains(summary, []byte("Skipped operators: 3    (Status counts exclude skipped operators)\nREADY operators: 1/2\nPLCC:")) {
			t.Fatalf("incorrect readiness ratio or position: %s", summary)
		}
		data, err := os.ReadFile(filepath.Join(dir, "assessment.json"))
		if err != nil {
			t.Fatal(err)
		}
		var report plcccheck.Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		want := plcccheck.Summary{Total: 5, NonSkipped: 2, Skipped: 3, FullyOK: 1,
			PLCC: plcccheck.PLCCCounts{OK: 2}, Catalog: &plcccheck.CatalogCounts{OK: 1, NoBundles: 1}}
		if !reflect.DeepEqual(report.Summary, want) {
			t.Fatalf("incorrect counts: %+v", report.Summary)
		}
		var names []string
		for _, pkg := range report.Assessment.Packages {
			names = append(names, pkg.Name)
			if pkg.Name == "mixed" && (pkg.Action != plcccheck.Skipped || pkg.PLCC != plcccheck.PLCCIncomplete || len(pkg.Issues) == 0) {
				t.Fatalf("missing skipped evidence: %+v", pkg)
			}
			if pkg.Name == "validator" && (pkg.SkipReason != "Shared exception" || len(pkg.Failures) == 0) {
				t.Fatalf("lost validator failure or note: %+v", pkg)
			}
		}
		if !reflect.DeepEqual(names, []string{"full", "mixed", "validator", "bundle-only", "no-bundles"}) {
			t.Fatalf("selection order or contents changed: %v", names)
		}
		_, details, _ := strings.Cut(string(stdout), "\nDetails\n")
		if !strings.Contains(details, "[SKIPPED] Shared exception") || strings.Contains(details, "[plcc-version-missing]") || strings.Contains(details, "[plcc-validation;") {
			t.Fatalf("incorrect skipped details: %s", details)
		}
		for _, artifact := range []string{"validation.jsonl", "fbc-output.yaml", "catalog-packages.txt"} {
			assertFilesEqual(t, filepath.Join(dir, artifact), filepath.Join(baseline, artifact))
		}
		data, err = os.ReadFile(filepath.Join(dir, "slack-payload.json"))
		if err != nil {
			t.Fatal(err)
		}
		var payload plcccheck.SlackPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		var slack strings.Builder
		for _, block := range payload.Blocks {
			if block.Text != nil {
				slack.WriteString(block.Text.Text)
			}
			for _, element := range block.Elements {
				for _, leaf := range element.Elements {
					slack.WriteString(leaf.Text)
				}
			}
			slack.WriteByte('\n')
		}
		for _, expected := range []string{"Selected operators (report.yaml)", "Skipped operators: 3    (Status counts exclude skipped operators)\nREADY operators: 1/2\nPLCC:", "➖ SKIPPED - operators excluded from action reporting 3/5\nmixed,validator,bundle-only", "[SKIPPED] Shared exception"} {
			if !strings.Contains(slack.String(), expected) {
				t.Errorf("Slack missing %q", expected)
			}
		}
		// Omitting selected still includes every catalog/PLCC operator; skip
		// names outside that union do not create new report rows.
		if err := os.WriteFile(config, []byte(policy), 0644); err != nil {
			t.Fatal(err)
		}
		stdout, code = run(t, append(common, "-o", dir)...)
		if code != 0 || !bytes.Contains(stdout, []byte("Total operators: 14\nSkipped operators: 3    (Status counts exclude skipped operators)\nREADY operators: 1/11\n")) {
			t.Fatalf("unselected configuration changed all-operator scope: code %d\n%s", code, stdout)
		}
	})
	t.Run("full offline report", func(t *testing.T) {
		dir := t.TempDir()
		stdout, code := run(t, "-i", "../../internal/plcccheck/testdata/plcc.json", "--catalog-input", "../../internal/plcccheck/testdata/catalog.json",
			"--validators", "syntax,catalog", "-o", dir)
		if code != 0 {
			t.Fatalf("findings failed command: %d", code)
		}
		golden, err := os.ReadFile("testdata/plcc-check/command-summary.txt")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stdout, golden) {
			t.Fatalf("summary differs from golden:\n%s", stdout)
		}
		assertFilesEqual(t, filepath.Join(dir, "summary.txt"), "testdata/plcc-check/command-summary.txt")
		data, err := os.ReadFile(filepath.Join(dir, "assessment.json"))
		if err != nil {
			t.Fatal(err)
		}
		var report plcccheck.Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		if report.Summary.Total != 14 || report.Summary.FullyOK != 1 || report.Summary.PLCC.Incomplete != 1 {
			t.Fatalf("unexpected JSON counts: %+v", report.Summary)
		}
	})
	t.Run("real opm and rendered input agree", func(t *testing.T) {
		rendered, err := exec.CommandContext(t.Context(), "opm", "render", "--output=json", "--", "testdata/catalog-fbc-versions").Output()
		if err != nil {
			t.Fatal(err)
		}
		input := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(input, rendered, 0644); err != nil {
			t.Fatal(err)
		}
		common := []string{"-i", "testdata/plcc.json", "--validators", "none"}
		fromImage, imageCode := run(t, append(common, "-o", t.TempDir(), "--catalog-image", "testdata/catalog-fbc-versions")...)
		fromJSON, jsonCode := run(t, append(common, "-o", t.TempDir(), "--catalog-input", input)...)
		// Only the recorded input source differs between these reports.
		imageSource := []byte("Catalog image: testdata/catalog-fbc-versions\n")
		jsonSource := []byte("Catalog input: " + input + "\n")
		if !bytes.Contains(fromImage, imageSource) || !bytes.Contains(fromJSON, jsonSource) {
			t.Fatal("catalog source missing from report")
		}
		fromImage = bytes.Replace(fromImage, imageSource, nil, 1)
		fromJSON = bytes.Replace(fromJSON, jsonSource, nil, 1)
		if imageCode != 0 || jsonCode != 0 || !bytes.Equal(fromImage, fromJSON) {
			t.Fatalf("rendered input and opm diverged (codes %d, %d)", imageCode, jsonCode)
		}
	})
	t.Run("no translatable products is still a report", func(t *testing.T) {
		dir := t.TempDir()
		stdout, code := run(t, "-i", "testdata/untranslatable.json", "--validators", "none", "-o", dir)
		if code != 0 {
			t.Fatalf("findings exited %d", code)
		}
		summary, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stdout, summary) {
			t.Fatal("stdout differs from summary.txt")
		}
		report := readAssessment(t, dir)
		if report.Summary.Total != 1 || report.Summary.PLCC.Invalid != 1 || report.Summary.PLCC.OK != 0 {
			t.Fatalf("unexpected summary counts: %+v", report.Summary)
		}
		pkg := assessedPackage(t, report, "untranslatable-operator")
		if pkg.PLCC != plcccheck.PLCCInvalid || pkg.Action != plcccheck.FixPLCC || len(pkg.Failures) != 1 {
			t.Fatalf("unexpected untranslatable assessment: %+v", pkg)
		}
		failure := pkg.Failures[0]
		if failure.Stage != plcccheck.FBCTranslation || !strings.Contains(strings.Join(failure.Reasons, "\n"), "FBC-VER-01") {
			t.Fatalf("missing version conversion failure: %+v", failure)
		}
		for _, reason := range failure.Reasons {
			if !bytes.Contains(summary, []byte(reason)) {
				t.Errorf("summary missing conversion reason %q", reason)
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, "fbc-output.yaml"))
		if err != nil || len(data) != 0 {
			t.Fatalf("unexpected FBC: %s (%v)", data, err)
		}
	})
	t.Run("fatal input error", func(t *testing.T) {
		stdout, code := run(t, "-i", "not-a-file", "-o", t.TempDir())
		if code != 1 || len(stdout) != 0 {
			t.Fatalf("fatal error: code %d, stdout %s", code, stdout)
		}
	})
}

func TestPLCCCheckLifecycleMismatch(t *testing.T) {
	fixture, err := os.ReadFile("testdata/catalog-fbc-match/full/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	operators := filepath.Join(t.TempDir(), "operators.txt")
	if err := os.WriteFile(operators, []byte("full\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var baselineFBC []byte
	for _, mismatch := range []bool{false, true} {
		for _, mode := range []string{"--catalog-input", "--catalog-image"} {
			name := fmt.Sprintf("%s/mismatch=%t", mode, mismatch)
			t.Run(name, func(t *testing.T) {
				input := append([]byte(nil), fixture...)
				if mismatch {
					input = bytes.Replace(input, []byte("2020-12-31"), []byte("2020-12-30"), 1)
				}
				catalogDir := t.TempDir()
				catalogPath := filepath.Join(catalogDir, "catalog.json")
				if err := os.WriteFile(catalogPath, input, 0644); err != nil {
					t.Fatal(err)
				}
				if mode == "--catalog-image" {
					catalogPath = catalogDir
				}
				dir := t.TempDir()
				stdout, stderr, code := runPlccCheck(t, "-i", "../../internal/plcccheck/testdata/plcc.json", "--validators", "syntax,catalog",
					mode, catalogPath, "--webhook", "summary,table,list,details", "-o", dir, operators)
				if code != 0 || len(stderr) != 0 {
					t.Fatalf("exit=%d stderr=%s", code, stderr)
				}
				r := readAssessment(t, dir)
				pkg := assessedPackage(t, r, "full")
				wantReady, wantMismatch := 1, 0
				wantStatus, wantAction := plcccheck.CatalogOK, plcccheck.OK
				if mismatch {
					wantReady, wantMismatch = 0, 1
					wantStatus, wantAction = plcccheck.CatalogMismatch, plcccheck.BuildOperator
				}
				if pkg.Catalog.MatchesPLCC == nil || *pkg.Catalog.MatchesPLCC == mismatch || pkg.Catalog.Status() != wantStatus || pkg.Action != wantAction ||
					r.Summary.FullyOK != wantReady || r.Summary.Catalog.Mismatch != wantMismatch || r.Summary.NonSkipped != 1 {
					t.Fatalf("incorrect mismatch assessment: %+v, package %+v", r.Summary, pkg)
				}
				if bytes.Contains(stdout, []byte("[catalog-lifecycle-mismatch]")) != mismatch {
					t.Fatalf("mismatch finding missing or unexpected: %s", stdout)
				}
				if mismatch {
					want := plcccheck.LifecycleDifference{Path: `lifecycles[0].versions[name="1.10"].phases[0].endDate`,
						PLCC: json.RawMessage(`"2020-12-31"`), Catalog: json.RawMessage(`"2020-12-30"`)}
					if !reflect.DeepEqual(pkg.Catalog.LifecycleDifferences, []plcccheck.LifecycleDifference{want}) {
						t.Fatalf("JSON lost mismatch details: %+v", pkg.Catalog.LifecycleDifferences)
					}
				} else if len(pkg.Catalog.LifecycleDifferences) != 0 {
					t.Fatalf("matching lifecycle has differences: %+v", pkg.Catalog.LifecycleDifferences)
				}
				payload, err := os.ReadFile(filepath.Join(dir, "slack-payload.json"))
				if err != nil || bytes.Contains(payload, []byte("[catalog-lifecycle-mismatch]")) != mismatch ||
					!bytes.Contains(payload, []byte(fmt.Sprintf("MISMATCH %d", wantMismatch))) {
					t.Fatalf("incorrect Slack mismatch report: %s, %v", payload, err)
				}
				if mismatch {
					var slack struct {
						Blocks []struct{ Text struct{ Text string } }
					}
					if err := json.Unmarshal(payload, &slack); err != nil {
						t.Fatal(err)
					}
					var slackText strings.Builder
					for _, block := range slack.Blocks {
						slackText.WriteString(block.Text.Text)
					}
					for _, output := range []string{string(stdout), slackText.String()} {
						if !strings.Contains(output, `lifecycles[0].versions[name="1.10"].phases[0].endDate: catalog="2020-12-30"; generated from PLCC="2020-12-31"`) {
							t.Fatalf("missing field difference: %s", output)
						}
					}
				}
				fbc, err := os.ReadFile(filepath.Join(dir, "fbc-output.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if baselineFBC == nil {
					baselineFBC = fbc
				} else if !bytes.Equal(baselineFBC, fbc) {
					t.Fatal("catalog comparison changed generated FBC output")
				}
			})
		}
	}
}
