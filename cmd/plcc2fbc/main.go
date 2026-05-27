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
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

const packageNotFound = 1

func main() {
	var format string
	var logPath string
	var packages string
	var dumpPLCC bool
	var inputPath string

	flag.StringVarP(&format, "output", "o", "json", "output format: json or yaml")
	flag.StringVarP(&logPath, "log", "l", "", "write operational logs to a file (default: stdout)")
	flag.StringVarP(&packages, "package", "p", "", "comma-separated list of package names to include")
	flag.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.BoolVar(&dumpPLCC, "dump-plcc", false, "dump filtered PLCC JSON instead of generating FBC")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <output-file>\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	var logWriter io.Writer = os.Stdout
	if logPath != "" {
		lf, err := os.Create(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create log file: %v\n", err)
			os.Exit(1)
		}
		defer lf.Close()
		logWriter = lf
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter, nil)))

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	writePath := flag.Arg(0)
	if format != "json" && format != "yaml" {
		slog.Error("invalid output format", "flag", "-o", "value", format, "allowed", "json,yaml")
		os.Exit(1)
	}

	var (
		catalog *plcc.Catalog
		err     error
	)
	if inputPath != "" {
		catalog, err = plcc.Load(inputPath)
	} else {
		catalog, err = plcc.Fetch()
	}
	if err != nil {
		slog.Error("failed to load PLCC data", "error", err)
		os.Exit(1)
	}

	slog.Info("fetched products from PLCC", "count", catalog.Len())
	if packages != "" {
		var names []string
		for _, name := range strings.Split(packages, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
		catalog.FilterByPackageNames(names)
	} else {
		catalog.FilterPackages()
	}
	slog.Info("filtered packages", "count", catalog.Len())
	catalog.SortByPackage()

	if dumpPLCC {
		if err := catalog.Dump(writePath); err != nil {
			slog.Error("failed to write PLCC dump", "path", writePath, "error", err)
			os.Exit(1)
		}
		slog.Info("wrote PLCC dump", "count", catalog.Len(), "path", writePath)
		return
	}

	f, err := os.Create(writePath)
	if err != nil {
		slog.Error("failed to create output file", "path", writePath, "error", err)
		os.Exit(1)
	}
	defer f.Close()

	blobCount, err := fbc.GenerateFBC(catalog.Data, f, os.Stderr, format)
	if err != nil {
		slog.Error("failed to generate FBC", "error", err)
		os.Exit(1)
	}
	if blobCount == 0 {
		slog.Warn("no valid FBC data found")
		os.Exit(packageNotFound)
	}
	slog.Info("wrote FBC data", "count", blobCount, "path", writePath, "format", format)
}
