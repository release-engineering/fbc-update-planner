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

// TestPlccCheckOperatorsFile runs plcc-check.sh against a small, fixed
// operators file (one passing, one failing, one missing package) so the
// generated files can be compared against small, reviewable golden fixtures.
func TestPlccCheckOperatorsFile(t *testing.T) {
	outDir := t.TempDir()
	_, stderr, exitCode := runPlccCheck(t,
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

	gotValidation, err := os.ReadFile(filepath.Join(outDir, "validation.jsonl"))
	if err != nil {
		t.Fatalf("reading validation.jsonl: %v", err)
	}
	wantValidation, err := os.ReadFile("testdata/plcc-check/operators-validation.jsonl")
	if err != nil {
		t.Fatalf("reading golden validation.jsonl: %v", err)
	}
	if string(gotValidation) != string(wantValidation) {
		t.Errorf("validation.jsonl mismatch:\ngot:  %s\nwant: %s", gotValidation, wantValidation)
	}

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
			want = map[string]any{"count": float64(2)}
		case "PLCC product validation":
			want = map[string]any{"passed": float64(1), "filtered": float64(1)}
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
		"Total:         142",
		"Passed:        61",
		"Not found:     0",
		"Duplicated:    0",
		"With issues:   81",
	} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("summary.txt missing expected line %q; full summary:\n%s", want, summary)
		}
	}
}
