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

	flag "github.com/spf13/pflag"
)

func runFetchCommand(args []string) error {
	var inputPath string
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVarP(&inputPath, "input", "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s fetch [flags] <snapshot.json>\n\nWrite raw PLCC data without filtering or validation. The parent directory of <snapshot.json> must already exist.\n\nFlags:\n", os.Args[0])
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("fetch requires a snapshot output path")
	}
	path := flags.Arg(0)
	if err := validateOutputPath(path, false); err != nil {
		return fmt.Errorf("invalid snapshot path: %w", err)
	}
	catalog, err := loadCatalog(inputPath)
	if err != nil {
		return err
	}
	if err := catalog.Dump(path); err != nil {
		return fmt.Errorf("writing PLCC snapshot: %w", err)
	}
	slog.Info("wrote PLCC snapshot", "count", catalog.Len(), "path", path)
	return nil
}
