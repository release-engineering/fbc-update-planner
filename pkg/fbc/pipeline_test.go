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

package fbc

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

// TestReferenceFile runs the full pipeline on the reference PLCC testdata/plcc.json file.
// The result is compared against the expected FBC file output (testdata/reference-fbc.yaml).
func TestReferenceFile(t *testing.T) {
	catalog, err := plcc.Load("testdata/plcc.json")
	if err != nil {
		t.Fatalf("loading PLCC test data: %v", err)
	}

	catalog.FilterPackages()
	catalog.SortByPackage()

	var buf bytes.Buffer
	GenerateFBC(catalog.Data, &buf, io.Discard)

	want, err := os.ReadFile("testdata/reference-fbc.yaml")
	if err != nil {
		t.Fatalf("reading reference file: %v", err)
	}

	if buf.String() != string(want) {
		t.Errorf("FBC output does not match reference file (got %d bytes, want %d bytes)", buf.Len(), len(want))
		os.WriteFile("testdata/actual-fbc.yaml", buf.Bytes(), 0644)
		t.Log("actual output written to testdata/actual-fbc.yaml")
	}
}
