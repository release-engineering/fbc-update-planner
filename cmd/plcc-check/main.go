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
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/release-engineering/fbc-update-planner/internal/plcccheck"
	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

type options struct {
	output, input, catalogImage, catalogInput, operatorsFile string
	configFile                                               string
	validators                                               []string
	dumpPLCC                                                 bool
	slack                                                    *plcccheck.SlackOptions
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// Findings are successful report results. Only argument, input, assessment and
// output errors fail the command; this command is not the future build gate.
func run(ctx context.Context, args []string, stdout io.Writer) (err error) {
	opts, err := parseOptions(args, stdout)
	if err != nil || opts == nil {
		return err
	}
	if err := os.MkdirAll(opts.output, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	// A failed rerun must not leave a previous payload available for posting.
	payloadPath := filepath.Join(opts.output, "slack-payload.json")
	if err := removeArtifact(payloadPath); err != nil {
		return err
	}
	logFile, err := os.Create(filepath.Join(opts.output, "slog.json"))
	if err != nil {
		return fmt.Errorf("create run log: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	defer func() {
		if err != nil {
			logger.Error("assessment failed", "error", err)
		}
		err = errors.Join(err, logFile.Close())
		if err != nil {
			err = errors.Join(err, removeArtifact(payloadPath))
		}
	}()
	logger.Info("assessment starting", "input", opts.input, "catalogImage", opts.catalogImage, "catalogInput", opts.catalogInput, "validators", opts.validators)

	var names []string
	var skips map[string]string
	if opts.configFile != "" {
		names, skips, err = readConfig(opts.configFile)
		if err != nil {
			return err
		}
		if opts.slack != nil && names != nil {
			opts.slack.Scope = "Selected operators (" + filepath.Base(opts.configFile) + ")"
		}
	}
	if opts.operatorsFile != "" {
		names, err = readOperators(opts.operatorsFile)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var source *plcc.Catalog
	if opts.input == "" {
		source, err = plcc.FetchContext(ctx)
	} else {
		source, err = plcc.Load(opts.input)
	}
	if err != nil {
		return fmt.Errorf("load PLCC: %w", err)
	}
	logger.Info("loaded PLCC snapshot", "products", len(source.Data))
	inventory, err := loadCatalog(ctx, opts)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	assessment, err := plcccheck.Assess(source, inventory, plcc.DatasetOptions{Packages: names, Validators: opts.validators})
	if err != nil {
		return fmt.Errorf("assess lifecycle data: %w", err)
	}
	if err := assessment.ApplySkips(skips); err != nil {
		return err
	}
	result := plcccheck.NewReport(assessment)
	result.CatalogImage = opts.catalogImage
	result.CatalogInput = opts.catalogInput
	var payload *plcccheck.SlackPayload
	if opts.slack != nil {
		payload, err = result.Slack(*opts.slack)
		if err != nil {
			return err
		}
	}
	summary := result.Text()
	if err := writeArtifacts(opts, assessment, result, inventory, payload, summary); err != nil {
		return err
	}
	if _, err := io.WriteString(stdout, summary); err != nil {
		return fmt.Errorf("write summary to stdout: %w", err)
	}
	logger.Info("assessment complete", "operators", result.Summary.Total, "skipped", result.Summary.Skipped, "fullyOK", result.Summary.FullyOK)
	return nil
}

func parseOptions(args []string, stdout io.Writer) (*options, error) {
	opts := &options{}
	flags := pflag.NewFlagSet("plcc-check", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var validators, webhook string
	var help bool
	flags.StringVarP(&opts.output, "output", "o", ".", "output directory")
	flags.StringVarP(&opts.input, "input", "i", "", "read a PLCC JSON file instead of fetching the API")
	flags.StringVar(&opts.configFile, "config", "", "YAML configuration with selected operators and skip groups; mutually exclusive with operators-file")
	flags.BoolVar(&opts.dumpPLCC, "plcc", false, "write filtered PLCC instead of FBC; assessment still checks translation")
	flags.StringVar(&validators, "validators", "all", "comma-separated validator labels or groups: all, none, syntax, semantic, catalog")
	flags.StringVar(&opts.catalogImage, "catalog-image", "", "catalog image or directory to render with opm")
	flags.StringVar(&opts.catalogInput, "catalog-input", "", "read rendered catalog JSON without opm")
	flags.StringVar(&webhook, "webhook", "", "write Slack payload sections: summary (counts), table (operators), list (CSV by action), details (findings); does not post")
	flags.BoolVarP(&help, "help", "h", false, "show help")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if help {
		_, err := fmt.Fprint(stdout, "Usage: plcc-check [flags] [operators-file]\n\nWithout an operators file or a config selection, assess all PLCC and catalog packages.\nFindings exit successfully; execution errors exit with status 1.\n\n"+flags.FlagUsages())
		return nil, err
	}
	if flags.NArg() > 1 {
		return nil, fmt.Errorf("expected at most one operators file")
	}
	if opts.output == "" {
		return nil, fmt.Errorf("output directory must not be empty")
	}
	if flags.Changed("catalog-image") && flags.Changed("catalog-input") {
		return nil, fmt.Errorf("--catalog-image and --catalog-input are mutually exclusive")
	}
	for _, name := range []string{"input", "catalog-image", "catalog-input", "config"} {
		if flags.Changed(name) && flags.Lookup(name).Value.String() == "" {
			return nil, fmt.Errorf("--%s must not be empty", name)
		}
	}
	opts.validators = strings.Split(validators, ",")
	if flags.NArg() == 1 {
		if opts.configFile != "" {
			return nil, fmt.Errorf("--config and operators-file are mutually exclusive")
		}
		opts.operatorsFile = flags.Arg(0)
		if opts.operatorsFile == "" {
			return nil, fmt.Errorf("operators file must not be empty")
		}
	}
	if flags.Changed("webhook") {
		opts.slack = &plcccheck.SlackOptions{Scope: "All operators"}
		for _, section := range strings.Split(webhook, ",") {
			switch section {
			case "summary":
				opts.slack.Summary = true
			case "table":
				opts.slack.Table = true
			case "list":
				opts.slack.List = true
			case "details":
				opts.slack.Details = true
			default:
				return nil, fmt.Errorf("unsupported webhook section %q: use summary,table,list,details", section)
			}
		}
		if opts.operatorsFile != "" {
			opts.slack.Scope = "Selected operators (" + filepath.Base(opts.operatorsFile) + ")"
		}
		server, repo, runID := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
		if server == "" || repo == "" || runID == "" {
			return nil, fmt.Errorf("--webhook requires GITHUB_SERVER_URL, GITHUB_REPOSITORY, and GITHUB_RUN_ID")
		}
		opts.slack.RunURL = strings.TrimRight(server, "/") + "/" + repo + "/actions/runs/" + runID
	}
	return opts, nil
}

func readOperators(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read operators file: %w", err)
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		name, _, _ := strings.Cut(line, "#")
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no operator names found in %s", path)
	}
	return names, nil
}

func loadCatalog(ctx context.Context, opts *options) (*catalog.Inventory, error) {
	if opts.catalogImage != "" {
		return catalog.Render(ctx, opts.catalogImage)
	}
	if opts.catalogInput == "" {
		return nil, nil
	}
	file, err := os.Open(opts.catalogInput)
	if err != nil {
		return nil, fmt.Errorf("open rendered catalog: %w", err)
	}
	defer func() { _ = file.Close() }()
	inventory, err := catalog.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("parse rendered catalog: %w", err)
	}
	return inventory, nil
}
