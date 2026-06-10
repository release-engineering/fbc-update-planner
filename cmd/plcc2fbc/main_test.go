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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flag "github.com/spf13/pflag"
)

var testdataInput = filepath.Join("..", "..", "pkg", "fbc", "testdata", "plcc.json")

func resetFlags(args []string) {
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
}

func TestRun(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantErr      string
		wantNotFound bool
	}{
		{
			name:    "missing output file",
			args:    []string{"plcc2fbc"},
			wantErr: "missing output file",
		},
		{
			name:    "invalid format",
			args:    []string{"plcc2fbc", "-o", "xml", "/dev/null"},
			wantErr: "unsupported format",
		},
		{
			name:    "bad input path",
			args:    []string{"plcc2fbc", "-i", "/nonexistent/plcc.json", "/dev/null"},
			wantErr: "no such file",
		},
		{
			name:         "no matching packages",
			args:         []string{"plcc2fbc", "-i", testdataInput, "-p", "nonexistent-package", t.TempDir() + "/out.json"},
			wantNotFound: true,
		},
		{
			name:    "output path parent dir does not exist",
			args:    []string{"plcc2fbc", "-i", testdataInput, "/nonexistent-dir/output.json"},
			wantErr: "parent directory",
		},
		{
			name:    "log path parent dir does not exist",
			args:    []string{"plcc2fbc", "-i", testdataInput, "-l", "/nonexistent-dir/run.log", "/dev/null"},
			wantErr: "parent directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags(tt.args)

			err := run()
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.wantNotFound {
				if !errors.Is(err, errPackageNotFound) {
					t.Errorf("expected errPackageNotFound, got: %v", err)
				}
				return
			}

			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestRunSuccess(t *testing.T) {
	tests := []struct {
		name   string
		args   func(outFile string) []string
		checks func(t *testing.T, outFile string)
	}{
		{
			name: "json",
			args: func(out string) []string {
				return []string{"plcc2fbc", "-i", testdataInput, "-o", "json", out}
			},
		},
		{
			name: "json-pretty",
			args: func(out string) []string {
				return []string{"plcc2fbc", "-i", testdataInput, "-o", "json-pretty", out}
			},
		},
		{
			name: "yaml",
			args: func(out string) []string {
				return []string{"plcc2fbc", "-i", testdataInput, "-o", "yaml", out}
			},
		},
		{
			name: "dump-plcc",
			args: func(out string) []string {
				return []string{"plcc2fbc", "-i", testdataInput, "--dump-plcc", out}
			},
		},
		{
			name: "with log file",
			args: func(out string) []string {
				logFile := filepath.Join(filepath.Dir(out), "run.log")
				return []string{"plcc2fbc", "-i", testdataInput, "-l", logFile, out}
			},
			checks: func(t *testing.T, outFile string) {
				logFile := filepath.Join(filepath.Dir(outFile), "run.log")
				info, err := os.Stat(logFile)
				if err != nil {
					t.Fatalf("log file not created: %v", err)
				}
				if info.Size() == 0 {
					t.Error("log file is empty")
				}
			},
		},
		{
			name: "package filter with match",
			args: func(out string) []string {
				return []string{"plcc2fbc", "-i", testdataInput, "-p", "rhacs-operator", out}
			},
			checks: func(t *testing.T, outFile string) {
				data, err := os.ReadFile(outFile)
				if err != nil {
					t.Fatalf("reading output: %v", err)
				}
				content := string(data)
				if !strings.Contains(content, "rhacs-operator") {
					t.Error("output should contain rhacs-operator")
				}
				if strings.Contains(content, "advanced-cluster-management") {
					t.Error("output should not contain other packages")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outFile := filepath.Join(t.TempDir(), "output")
			resetFlags(tt.args(outFile))

			if err := run(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			info, err := os.Stat(outFile)
			if err != nil {
				t.Fatalf("output file not created: %v", err)
			}
			if info.Size() == 0 {
				t.Error("output file is empty")
			}

			if tt.checks != nil {
				tt.checks(t, outFile)
			}
		})
	}
}

func TestValidateOutputPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name: "valid path in temp dir",
			path: filepath.Join(t.TempDir(), "output.json"),
		},
		{
			name: "valid path in current dir",
			path: "output.json",
		},
		{
			name:    "parent dir does not exist",
			path:    "/nonexistent-dir/sub/output.json",
			wantErr: "does not exist",
		},
		{
			name: "parent is a file not a directory",
			path: func() string {
				f := filepath.Join(t.TempDir(), "afile")
				if err := os.WriteFile(f, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(f, "output.json")
			}(),
			wantErr: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOutputPath(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantErr)
			}
		})
	}
}
