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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

const packageNotFound = 1

func main() {
	var writePath string
	var format string
	var logPath string
	var plccDumpPath string
	var inputPath string

	flag.StringVar(&writePath, "w", "", "write FBC data to a file (required)")
	flag.StringVar(&format, "o", "json", "output format: json or yaml")
	flag.StringVar(&logPath, "l", "", "write operational logs to a file (default: stdout)")
	flag.StringVar(&inputPath, "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.StringVar(&plccDumpPath, "dump-plcc", "", "dump filtered PLCC JSON to a file")
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

	if writePath == "" {
		slog.Error("missing required flag", "flag", "-w")
		os.Exit(1)
	}
	if format != "json" && format != "yaml" {
		slog.Error("invalid output format", "flag", "-o", "value", format, "allowed", "json,yaml")
		os.Exit(1)
	}

	f, err := os.Create(writePath)
	if err != nil {
		slog.Error("failed to create output file", "path", writePath, "error", err)
		os.Exit(1)
	}
	defer f.Close()
	output := f

	var catalog *plcc.Catalog
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
	catalog.FilterPackages()
	slog.Info("filtered packages", "count", catalog.Len())
	catalog.SortByPackage()

	if plccDumpPath != "" {
		if err := catalog.Dump(plccDumpPath); err != nil {
			slog.Error("failed to write PLCC dump", "path", plccDumpPath, "error", err)
			os.Exit(1)
		}
		slog.Info("wrote PLCC dump", "count", catalog.Len(), "path", plccDumpPath)
		return
	}

	blobCount, err := fbc.GenerateFBC(catalog.Data, output, os.Stderr, format)
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
