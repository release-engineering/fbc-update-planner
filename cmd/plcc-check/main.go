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
	"context"
	"errors"
	"fmt"
	"os"

	flag "github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/pkg/check"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var opts check.Options
	var help bool
	flags := flag.NewFlagSet("plcc-check", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVarP(&opts.OutputDir, "output", "o", ".", "output directory for generated files")
	flags.StringVarP(&opts.InputPath, "input", "i", "", "read PLCC JSON from a file instead of the API")
	flags.BoolVar(&opts.ValidateOnly, "plcc", false, "validate PLCC data without generating FBC")
	flags.StringVar(&opts.Validators, "validators", "all", "comma-separated PLCC validators to run")
	flags.StringVar(&opts.CatalogImage, "catalog-image", "", "catalog image or local FBC directory to assess using opm render")
	flags.StringVar(&opts.Webhook, "webhook", "", "Slack payload sections: summary,list")
	flags.BoolVarP(&help, "help", "h", false, "show help")
	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [operators-file]\n\nWithout an operators file, assess all PLCC packages and catalog packages.\n\nOptions:\n", os.Args[0])
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if help {
		flags.Usage()
		return nil
	}
	if flags.Changed("webhook") && opts.Webhook == "" {
		return fmt.Errorf("--webhook requires a comma-separated list of sections: summary,list")
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return fmt.Errorf("expected at most one operators file")
	}
	if flags.NArg() == 1 {
		opts.Operators = flags.Arg(0)
	}
	return check.Run(context.Background(), opts, os.Stdout, os.Stderr)
}
