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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "plcc2fbc-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating temp dir: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(tmp, "plcc2fbc")
	cmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/plcc2fbc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building plcc2fbc: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(exitCode)
}

func runBinary(t *testing.T, args ...string) (stdout, stderr []byte, exitCode int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return outBuf.Bytes(), errBuf.Bytes(), exitErr.ExitCode()
		}
		t.Fatalf("running binary: %v", err)
	}
	return outBuf.Bytes(), errBuf.Bytes(), 0
}

func extractPackageName(yamlDoc string) string {
	for _, line := range strings.SplitN(yamlDoc, "\n", 5) {
		if name, ok := strings.CutPrefix(line, "package: "); ok {
			return name
		}
	}
	return ""
}

func splitYAMLReference(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading reference file %s: %v", path, err)
	}

	segments := strings.Split(string(data), "---\n")
	result := make(map[string]string, len(segments))
	for _, seg := range segments {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		name := extractPackageName(seg)
		if name == "" {
			t.Fatalf("could not extract package name from YAML segment: %s", seg[:min(100, len(seg))])
		}
		result[name] = seg
	}
	return result
}

func TestSingleFileNoValidators(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"--validators", "none",
		"-o", "yaml",
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	want, err := os.ReadFile("testdata/reference-fbc.yaml")
	if err != nil {
		t.Fatalf("reading reference: %v", err)
	}

	if !bytes.Equal(got, want) {
		actualPath := filepath.Join(t.TempDir(), "actual-fbc.yaml")
		if wErr := os.WriteFile(actualPath, got, 0o644); wErr != nil {
			t.Logf("failed to write actual output: %v", wErr)
		}
		t.Errorf("output does not match reference (got %d bytes, want %d bytes)\nactual output: %s", len(got), len(want), actualPath)
	}
}

func TestSingleFileAllValidators(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"-o", "yaml",
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	want, err := os.ReadFile("testdata/reference-fbc-validated.yaml")
	if err != nil {
		t.Fatalf("reading reference: %v", err)
	}

	if !bytes.Equal(got, want) {
		actualPath := filepath.Join(t.TempDir(), "actual-fbc-validated.yaml")
		if wErr := os.WriteFile(actualPath, got, 0o644); wErr != nil {
			t.Logf("failed to write actual output: %v", wErr)
		}
		t.Errorf("output does not match reference (got %d bytes, want %d bytes)\nactual output: %s", len(got), len(want), actualPath)
	}
}

func testSplit(t *testing.T, referencePath string, extraArgs ...string) {
	t.Helper()
	refByPackage := splitYAMLReference(t, referencePath)

	pkgNames := make([]string, 0, len(refByPackage))
	for name := range refByPackage {
		pkgNames = append(pkgNames, name)
	}
	sort.Strings(pkgNames)

	splitDir := filepath.Join(t.TempDir(), "split")
	if err := os.Mkdir(splitDir, 0o755); err != nil {
		t.Fatalf("creating split dir: %v", err)
	}

	args := []string{
		"-i", "testdata/plcc.json",
		"-o", "yaml",
		"-p", strings.Join(pkgNames, ","),
		"--split", splitDir,
	}
	args = append(args, extraArgs...)

	_, stderr, exitCode := runBinary(t, args...)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	entries, err := os.ReadDir(splitDir)
	if err != nil {
		t.Fatalf("reading split dir: %v", err)
	}

	gotNames := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("unexpected file in split dir: %s", e.Name())
			continue
		}
		gotNames[e.Name()] = struct{}{}
	}

	if len(gotNames) != len(pkgNames) {
		t.Errorf("got %d package directories, want %d", len(gotNames), len(pkgNames))
	}

	for _, name := range pkgNames {
		if _, ok := gotNames[name]; !ok {
			t.Errorf("reference package %s was not produced by split", name)
			continue
		}

		got, err := os.ReadFile(filepath.Join(splitDir, name, "lifecycle.yaml"))
		if err != nil {
			t.Errorf("package %s: reading lifecycle.yaml: %v", name, err)
			continue
		}

		want := refByPackage[name]
		if string(got) != want {
			actualPath := filepath.Join(t.TempDir(), "actual-split", name+".yaml")
			if mkErr := os.MkdirAll(filepath.Dir(actualPath), 0o755); mkErr != nil {
				t.Logf("failed to create actual output dir: %v", mkErr)
			} else if wErr := os.WriteFile(actualPath, got, 0o644); wErr != nil {
				t.Logf("failed to write actual output: %v", wErr)
			}
			t.Errorf("package %s: split output does not match reference (got %d bytes, want %d bytes)\nactual: %s",
				name, len(got), len(want), actualPath)
		}
		delete(gotNames, name)
	}

	for name := range gotNames {
		t.Errorf("package %s: produced by split but not in reference", name)
	}
}

func TestSplitNoValidators(t *testing.T) {
	testSplit(t, "testdata/reference-fbc.yaml", "--validators", "none")
}

func TestSplitAllValidators(t *testing.T) {
	testSplit(t, "testdata/reference-fbc-validated.yaml")
}

func TestSingleFilePackageFilter(t *testing.T) {
	const targetPkg = "aws-efs-csi-driver-operator"

	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"--validators", "none",
		"-o", "yaml",
		"-p", targetPkg,
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	refByPackage := splitYAMLReference(t, "testdata/reference-fbc.yaml")
	want, ok := refByPackage[targetPkg]
	if !ok {
		t.Fatalf("package %s not found in reference file", targetPkg)
	}

	if string(got) != want {
		t.Errorf("filtered output does not match reference segment for %s (got %d bytes, want %d bytes)",
			targetPkg, len(got), len(want))
	}
}

func TestExitCode1_InvalidInput(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/nonexistent.json",
		"-o", "yaml",
		outFile,
	)
	if exitCode != 1 {
		t.Errorf("want exit code 1, got %d", exitCode)
	}
	if !strings.Contains(string(stderr), "Error:") {
		t.Errorf("stderr should contain 'Error:', got: %s", stderr)
	}
}

func TestExitCode2_NoFBCOutput(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/untranslatable.json",
		"--validators", "none",
		"-o", "yaml",
		outFile,
	)
	if exitCode != 2 {
		t.Errorf("want exit code 2, got %d", exitCode)
	}
	if !strings.Contains(string(stderr), "no FBC data generated") {
		t.Errorf("stderr should contain 'no FBC data generated', got: %s", stderr)
	}
}

func TestExitCode3_MissingPackages(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"-o", "yaml",
		"-p", "nonexistent-package-xyz",
		outFile,
	)
	if exitCode != 3 {
		t.Errorf("want exit code 3, got %d", exitCode)
	}
	if !strings.Contains(string(stderr), "requested packages not found") {
		t.Errorf("stderr should contain 'requested packages not found', got: %s", stderr)
	}
}

func TestDumpPLCC(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "dump.json")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"--dump-plcc",
		"--validators", "none",
		"-p", "aws-efs-csi-driver-operator",
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	if !json.Valid(got) {
		t.Fatal("output is not valid JSON")
	}

	if !strings.Contains(string(got), "aws-efs-csi-driver-operator") {
		t.Error("output should contain the requested package name")
	}
}

func TestAllowMissing(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	_, _, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"--validators", "none",
		"-o", "yaml",
		"-p", "aws-efs-csi-driver-operator,nonexistent-package-xyz",
		"--allow-missing",
		outFile,
	)
	if exitCode != 0 {
		t.Errorf("want exit code 0 with --allow-missing, got %d", exitCode)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	if !strings.Contains(string(got), "aws-efs-csi-driver-operator") {
		t.Error("output should contain the found package")
	}
}

func TestJSONOutput(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.json")
	_, stderr, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"--validators", "none",
		"-o", "json",
		"-p", "aws-efs-csi-driver-operator",
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", exitCode, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	if !json.Valid(got) {
		t.Fatal("output is not valid JSON")
	}

	if !strings.Contains(string(got), `"package":"aws-efs-csi-driver-operator"`) {
		t.Error("JSON output should contain the package name")
	}
}

func TestLogFlag(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.yaml")
	logFile := filepath.Join(t.TempDir(), "report.log")
	_, _, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"-o", "yaml",
		"-l", logFile,
		outFile,
	)
	if exitCode != 0 {
		t.Fatalf("exit code %d", exitCode)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	if len(logData) == 0 {
		t.Error("log file should not be empty")
	}

	for _, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("log line is not valid JSON: %s", line)
			break
		}
	}
}

func TestPermissive(t *testing.T) {
	strictFile := filepath.Join(t.TempDir(), "strict.yaml")
	_, _, exitCode := runBinary(t,
		"-i", "testdata/plcc.json",
		"-o", "yaml",
		strictFile,
	)
	if exitCode != 0 {
		t.Fatalf("strict run exit code %d", exitCode)
	}
	strictData, err := os.ReadFile(strictFile)
	if err != nil {
		t.Fatalf("reading strict output: %v", err)
	}
	strictCount := strings.Count(string(strictData), "package: ")

	permissiveFile := filepath.Join(t.TempDir(), "permissive.yaml")
	_, _, exitCode = runBinary(t,
		"-i", "testdata/plcc.json",
		"--permissive",
		"-o", "yaml",
		permissiveFile,
	)
	if exitCode != 0 {
		t.Fatalf("permissive run exit code %d", exitCode)
	}
	permissiveData, err := os.ReadFile(permissiveFile)
	if err != nil {
		t.Fatalf("reading permissive output: %v", err)
	}
	permissiveCount := strings.Count(string(permissiveData), "package: ")

	if permissiveCount < strictCount {
		t.Errorf("permissive output (%d packages) should have >= strict output (%d packages)",
			permissiveCount, strictCount)
	}
}

func TestListValidators(t *testing.T) {
	stdout, _, exitCode := runBinary(t, "--list-validators")
	if exitCode != 0 {
		t.Errorf("want exit code 0, got %d", exitCode)
	}

	out := string(stdout)
	if !strings.Contains(out, "Groups:") {
		t.Error("output should contain 'Groups:'")
	}
	if !strings.Contains(out, "Labels:") {
		t.Error("output should contain 'Labels:'")
	}
}
