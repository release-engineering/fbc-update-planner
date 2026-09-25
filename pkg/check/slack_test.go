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
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/release-engineering/fbc-update-planner/pkg/classify"
)

func TestSlackLargeReportLinksToCompleteArtifacts(t *testing.T) {
	data := runData{
		options: Options{CatalogImage: "local-catalog", Webhook: "summary,list"},
		runURL:  "https://example.test/run/1",
	}
	for i := range 100 {
		name := strings.Repeat("é", 1200) + fmt.Sprint(i)
		data.names = append(data.names, name)
		data.reports = append(data.reports, classify.OperatorReport{
			Package:       name,
			PrimaryAction: classify.ActionPLCCMissing,
			PLCC:          classify.PLCCStatusMissing,
			CatalogStatus: classify.CatalogStatusMissing,
		})
	}
	payload := renderSlack(data)
	if len(payload.Blocks) > 50 {
		t.Fatalf("got %d Slack blocks, want at most 50", len(payload.Blocks))
	}
	for i, block := range payload.Blocks {
		if len(block.Text.Text) > 3000 || !utf8.ValidString(block.Text.Text) {
			t.Errorf("block %d has %d bytes or invalid UTF-8", i, len(block.Text.Text))
		}
	}
	if !strings.Contains(payload.Blocks[len(payload.Blocks)-2].Text.Text, "complete artifacts") ||
		!strings.Contains(payload.Blocks[len(payload.Blocks)-1].Text.Text, data.runURL) {
		t.Errorf("bounded payload lost the truncation notice or artifact link")
	}
}
