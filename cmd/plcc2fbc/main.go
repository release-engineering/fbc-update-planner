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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

var errNoFBCOutput = errors.New("no FBC data generated")

func main() {
	if err := run(); err != nil {
		var pkgErr *plcc.PackagesNotFoundError
		switch {
		case errors.As(err, &pkgErr):
			fmt.Fprintln(os.Stderr, "Error: requested packages not found in PLCC data")
			os.Exit(3)
		case errors.Is(err, errNoFBCOutput):
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(2)
		default:
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return runConvertCommand(os.Args[1:])
}

func loadCatalog(inputPath string) (*plcc.Catalog, error) {
	if inputPath != "" {
		return plcc.Load(inputPath)
	}
	return plcc.Fetch()
}

// parseValidatorNames splits a comma-separated flag value into trimmed,
// non-empty validator names.
func parseValidatorNames(value string) []string {
	var names []string
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func validateOutputPath(path string, isDir bool) error {
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("directory %q does not exist", dir)
	}
	if err != nil {
		return fmt.Errorf("cannot access %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", dir)
	}
	return nil
}
