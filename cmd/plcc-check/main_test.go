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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/release-engineering/fbc-update-planner/internal/plcccheck"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

const sourceFixture = "../../internal/plcccheck/testdata/plcc.json"
const catalogFixture = "../../internal/plcccheck/testdata/catalog.json"

func writeTestFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTestFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readReport(t *testing.T, dir string) plcccheck.Report {
	t.Helper()
	var r plcccheck.Report
	if err := json.Unmarshal(readTestFile(t, dir, "assessment.json"), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func githubEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "org/repo")
	t.Setenv("GITHUB_RUN_ID", "42")
}

func TestRunArtifactsAndSelection(t *testing.T) {
	githubEnvironment(t)
	dir := t.TempDir()
	operators := writeTestFile(t, t.TempDir(), "operators", "# comment\n mixed # inline\nfull\nmixed\nabsent")
	args := []string{"-i", sourceFixture, "-o", dir, "--catalog-input", catalogFixture, "--validators", "syntax,catalog", "--webhook", "summary,table,list,details", operators}
	var stdout bytes.Buffer
	if err := run(t.Context(), args, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != string(readTestFile(t, dir, "summary.txt")) {
		t.Fatal("stdout differs from summary artifact")
	}
	r := readReport(t, dir)
	if r.CatalogInput != catalogFixture || r.CatalogImage != "" || !strings.Contains(stdout.String(), "Catalog input: "+catalogFixture) {
		t.Fatalf("missing catalog source in report: %+v", r)
	}
	var names []string
	for _, pkg := range r.Assessment.Packages {
		names = append(names, pkg.Name)
	}
	if !reflect.DeepEqual(names, []string{"mixed", "full", "absent"}) || r.Summary.FullyOK != 1 || r.Summary.PLCC.Incomplete != 1 {
		t.Fatalf("selection or counts incorrect: %v %+v", names, r.Summary)
	}
	yaml := string(readTestFile(t, dir, "fbc-output.yaml"))
	if !strings.Contains(yaml, "package: full") || !strings.Contains(yaml, "package: mixed") || strings.Count(yaml, "schema:") != 2 {
		t.Fatalf("unexpected translated output: %s", yaml)
	}
	if len(readTestFile(t, dir, "validation.jsonl")) != 0 {
		t.Fatal("missing source issues should not be logged as validator failures")
	}
	if got := string(readTestFile(t, dir, "catalog-packages.txt")); got != "empty-lifecycle\nfull\nlifecycle-only\nmixed\nstale\n" {
		t.Fatalf("catalog inventory artifact = %q", got)
	}
	slack := readTestFile(t, dir, "slack-payload.json")
	if !json.Valid(slack) || !bytes.Contains(slack, []byte("Details: mixed")) {
		t.Fatalf("invalid Slack report: %s", slack)
	}
	if !bytes.Contains(slack, []byte("Catalog input: "+catalogFixture)) {
		t.Fatal("catalog input missing from Slack summary")
	}
	if !bytes.Contains(readTestFile(t, dir, "slog.json"), []byte("assessment complete")) {
		t.Fatal("missing completion log")
	}

	// Reusing the output directory clears artifacts that no longer apply.
	if err := run(t.Context(), []string{"-i", sourceFixture, "-o", dir, "--plcc", "--validators", "none", operators}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if r := readReport(t, dir); r.CatalogImage != "" || r.CatalogInput != "" {
		t.Fatal("catalog metadata survived a run without catalog input")
	}
	for _, name := range []string{"fbc-output.yaml", "catalog-packages.txt", "slack-payload.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale %s remains: %v", name, err)
		}
	}
	var dump plcc.Catalog
	if err := json.Unmarshal(readTestFile(t, dir, "plcc-dump.json"), &dump); err != nil || len(dump.Data) != 2 {
		t.Fatalf("PLCC artifact: %+v, %v", dump, err)
	}
	r = readReport(t, dir)
	if r.Summary.Catalog != nil || r.Summary.FullyOK != 0 {
		t.Fatal("PLCC-only run claimed catalog readiness")
	}
}

func TestRunAllAndDumpStillAssessTranslation(t *testing.T) {
	for _, dump := range []bool{false, true} {
		dir := t.TempDir()
		args := []string{"-i", sourceFixture, "-o", dir, "--validators", "none", "--catalog-input", catalogFixture}
		if dump {
			args = append(args, "--plcc")
		}
		if err := run(t.Context(), args, io.Discard); err != nil {
			t.Fatal(err)
		}
		r := readReport(t, dir)
		if r.Summary.Total != 14 {
			t.Fatalf("all mode omitted catalog-only packages: %d", r.Summary.Total)
		}
		if len(r.Assessment.Validators) != 0 {
			t.Fatal("--validators none ignored")
		}
		decoder := json.NewDecoder(bytes.NewReader(readTestFile(t, dir, "validation.jsonl")))
		reasons := ""
		for {
			var result report.ValidationResult
			err := decoder.Decode(&result)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil || result.Valid {
				t.Fatalf("invalid validation record: %+v %v", result, err)
			}
			reasons += strings.Join(result.Reasons, "\n")
		}
		if !strings.Contains(reasons, "FBC-PHASE-01") || !strings.Contains(reasons, "FBC-VAL-02") {
			t.Fatalf("mandatory translation failures missing (dump=%v): %s", dump, reasons)
		}
	}
}

func TestRunErrors(t *testing.T) {
	githubEnvironment(t)
	tests := []struct {
		name  string
		extra []string
		setup func(*testing.T, string)
		want  string
	}{
		{"unknown flag", []string{"--unknown"}, nil, "unknown flag"},
		{"extra arguments", []string{"one", "two"}, nil, "at most one"},
		{"catalog conflict", []string{"--catalog-image", "image", "--catalog-input", catalogFixture}, nil, "mutually exclusive"},
		{"invalid validators", []string{"--validators", "bogus"}, nil, "bogus"},
		{"invalid webhook", []string{"--webhook", "summary,"}, nil, "unsupported webhook section"},
		{"missing source", []string{"-i", "missing-file.json"}, nil, "load PLCC"},
		{"missing catalog", []string{"--catalog-input", "missing-catalog.json"}, nil, "open rendered catalog"},
		{"missing operators", []string{"missing-operators"}, nil, "read operators file"},
		{"publish failure", nil, func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "assessment.json"), 0755); err != nil {
				t.Fatal(err)
			}
		}, "publish assessment.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			args := append([]string{"-i", sourceFixture, "-o", dir, "--validators", "syntax", "--webhook", "summary"}, tt.extra...)
			var stdout bytes.Buffer
			err := run(t.Context(), args, &stdout)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if stdout.Len() != 0 {
				t.Fatal("fatal error emitted a success report")
			}
			if _, err := os.Stat(filepath.Join(dir, "slack-payload.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fatal error left a Slack success payload")
			}
		})
	}
}

func TestRunFailedRerunAndMalformedCatalog(t *testing.T) {
	githubEnvironment(t)
	dir := t.TempDir()
	writeTestFile(t, dir, "slack-payload.json", "{}")
	badCatalog := writeTestFile(t, t.TempDir(), "catalog.json", "{")
	err := run(t.Context(), []string{"-i", sourceFixture, "-o", dir, "--catalog-input", badCatalog}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "parse rendered catalog") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "slack-payload.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("previous Slack payload survived failed assessment")
	}
	if !bytes.Contains(readTestFile(t, dir, "slog.json"), []byte("assessment failed")) {
		t.Fatal("missing fatal run log")
	}
}

func TestRunOpmFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("fake opm requires /bin/sh")
	}
	dir := t.TempDir()
	path := writeTestFile(t, dir, "opm", "#!/bin/sh\nprintf 'registry failed' >&2\nexit 9\n")
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	err := run(t.Context(), []string{"-i", sourceFixture, "-o", t.TempDir(), "--catalog-image", "image"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "registry failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunEmptySelectionFileAndHelp(t *testing.T) {
	operators := writeTestFile(t, t.TempDir(), "operators", "  # no operators\n")
	if err := run(t.Context(), []string{"-i", sourceFixture, "-o", t.TempDir(), operators}, io.Discard); err == nil || !strings.Contains(err.Error(), "no operator names") {
		t.Fatalf("error = %v", err)
	}
	var out bytes.Buffer
	if err := run(t.Context(), []string{"-h"}, &out); err != nil || !strings.Contains(out.String(), "Usage: plcc-check") {
		t.Fatalf("help: %s, %v", out.String(), err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output failed") }

func TestRunCancellationAndOutputFailure(t *testing.T) {
	githubEnvironment(t)
	dir := t.TempDir()
	args := []string{"-i", sourceFixture, "-o", dir, "--validators", "none", "--webhook", "summary"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := run(ctx, args, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := run(t.Context(), args, failingWriter{}); err == nil || !strings.Contains(err.Error(), "output failed") {
		t.Fatalf("stdout error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "slack-payload.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed output left Slack payload")
	}
}
