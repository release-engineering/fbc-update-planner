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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/release-engineering/fbc-update-planner/internal/plcccheck"
	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/report"
)

type artifact struct {
	name  string
	write func(io.Writer) error
}

// Render into a temporary directory first. Publish Slack last, only after all
// full artifacts have been written successfully. Individual renames are atomic;
// publishing the complete set is not a filesystem transaction.
func writeArtifacts(opts *options, assessment *plcccheck.Assessment, result *plcccheck.Report, inventory *catalog.Inventory, payload *plcccheck.SlackPayload, summary string) error {
	files := []artifact{
		{"summary.txt", func(w io.Writer) error { _, err := io.WriteString(w, summary); return err }},
		jsonArtifact("assessment.json", result),
		{"validation.jsonl", func(w io.Writer) error {
			for _, pkg := range assessment.Packages {
				for _, failure := range pkg.Failures {
					if err := report.LogResults(w, report.ValidationResult{PackageName: pkg.Name, Valid: false, Reasons: failure.Reasons}); err != nil {
						return err
					}
				}
			}
			return nil
		}},
	}
	unused := "plcc-dump.json"
	if opts.dumpPLCC {
		files = append(files, jsonArtifact("plcc-dump.json", assessment.FilteredPLCC))
		unused = "fbc-output.yaml"
	} else {
		files = append(files, artifact{"fbc-output.yaml", func(w io.Writer) error {
			return (fbc.YAMLWriter{}).Write(w, assessment.FBC...)
		}})
	}
	if inventory != nil {
		files = append(files, artifact{"catalog-packages.txt", func(w io.Writer) error {
			var names []string
			for name, pkg := range inventory.Packages {
				if pkg.HasLifecycle {
					names = append(names, name)
				}
			}
			slices.Sort(names)
			for _, name := range names {
				if _, err := fmt.Fprintln(w, name); err != nil {
					return err
				}
			}
			return nil
		}})
	}
	if payload != nil {
		files = append(files, jsonArtifact("slack-payload.json", payload))
	}
	stage, err := os.MkdirTemp(opts.output, ".plcc-check-*")
	if err != nil {
		return fmt.Errorf("stage artifacts: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for _, file := range files {
		if err := writeArtifact(filepath.Join(stage, file.name), file.write); err != nil {
			return fmt.Errorf("write %s: %w", file.name, err)
		}
	}
	if err := removeArtifact(filepath.Join(opts.output, unused)); err != nil {
		return err
	}
	if inventory == nil {
		if err := removeArtifact(filepath.Join(opts.output, "catalog-packages.txt")); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err := os.Rename(filepath.Join(stage, file.name), filepath.Join(opts.output, file.name)); err != nil {
			return fmt.Errorf("publish %s: %w", file.name, err)
		}
	}
	return nil
}

func jsonArtifact(name string, value any) artifact {
	return artifact{name, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}}
}

func writeArtifact(path string, write func(io.Writer) error) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(write(file), file.Close())
}

func removeArtifact(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale artifact %s: %w", path, err)
	}
	return nil
}
