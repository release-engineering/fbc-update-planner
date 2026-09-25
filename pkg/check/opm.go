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

package check

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/classify"
)

func renderCatalog(parent context.Context, image string) (*classify.CatalogData, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cmd := exec.CommandContext(ctx, "opm", "render", image)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening opm output: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting opm render: %w", err)
	}
	data, parseErr := catalog.ParseRender(stdout)
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, parseErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("opm render failed for %q: %w: %s", image, waitErr, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

func validateWebhook(sections string) error {
	if sections == "" {
		return nil
	}
	seen := make(map[string]bool)
	for _, section := range strings.Split(sections, ",") {
		if section == "" {
			return fmt.Errorf("--webhook requires a comma-separated list of sections: summary,list")
		}
		if section != "summary" && section != "list" {
			return fmt.Errorf("unsupported webhook section: %s (supported: summary,list)", section)
		}
		if seen[section] {
			return fmt.Errorf("duplicate webhook section: %s", section)
		}
		seen[section] = true
	}
	for _, name := range []string{"GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "GITHUB_RUN_ID"} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("--webhook requires GITHUB_SERVER_URL, GITHUB_REPOSITORY, and GITHUB_RUN_ID")
		}
	}
	return nil
}
