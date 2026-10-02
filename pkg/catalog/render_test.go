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

package catalog_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
)

// The fake executable exercises the public API without requiring opm or a
// registry. All script bodies use shell builtins, independent of PATH.
func fakeOpm(t *testing.T, body string) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("fake opm requires /bin/sh")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opm"), []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestRenderForwardsArgumentsAndEnvironment(t *testing.T) {
	// A reference remains a single positional argument even with shell syntax
	// or a leading dash. The adapter must never evaluate it through a shell.
	reference := "--catalog with spaces;$(ignored)"
	t.Setenv("CATALOG_TEST_REFERENCE", reference)
	auth := filepath.Join(t.TempDir(), "auth config.json")
	t.Setenv("CATALOG_TEST_AUTH", auth)
	t.Setenv("REGISTRY_AUTH_FILE", auth)
	fakeOpm(t, `
[ "$#" -eq 4 ]
[ "$1" = render ]
[ "$2" = --output=json ]
[ "$3" = -- ]
[ "$4" = "$CATALOG_TEST_REFERENCE" ]
[ "$REGISTRY_AUTH_FILE" = "$CATALOG_TEST_AUTH" ]
printf '%s\n' '`+bundleJSON+`'
printf '%s\n' 'a harmless warning' >&2
`)
	got, err := catalog.Render(t.Context(), reference)
	if err != nil {
		t.Fatal(err)
	}
	pkg := got.Packages["alpha"]
	if len(got.Packages) != 1 || len(pkg.Bundles) != 1 || pkg.Bundles[0].Version != "1.2.3" {
		t.Errorf("unexpected inventory: %#v", got.Packages)
	}
}

func TestRenderRejectsEmptyReference(t *testing.T) {
	for _, reference := range []string{"", " \n\t"} {
		got, err := catalog.Render(t.Context(), reference)
		if got != nil || err == nil || !strings.Contains(err.Error(), "reference must be nonempty") {
			t.Errorf("Render(%q) = (%#v, %v), want empty reference error", reference, got, err)
		}
	}
}

func TestRenderMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := catalog.Render(t.Context(), "catalog-reference")
	if got != nil || !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("Render() = (%#v, %v), want missing executable error", got, err)
	}
}

func TestRenderRequiresSuccessfulExit(t *testing.T) {
	fakeOpm(t, `
printf '%s\n' '`+bundleJSON+`'
printf '%s\n' 'registry request failed' >&2
exit 9
`)
	got, err := catalog.Render(t.Context(), "catalog-reference")
	if got != nil || err == nil {
		t.Fatalf("Render() = (%#v, %v), want nil inventory and error", got, err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 9 {
		t.Errorf("error = %v, want wrapped exit code 9", err)
	}
	for _, want := range []string{"catalog-reference", "registry request failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func TestRenderMalformedOutput(t *testing.T) {
	fakeOpm(t, `printf '%s' '{"schema":'`)
	got, err := catalog.Render(t.Context(), "catalog-reference")
	if got != nil || err == nil || !strings.Contains(err.Error(), "catalog record 1: unexpected EOF") {
		t.Errorf("Render() = (%#v, %v), want parsing error", got, err)
	}
}

func TestRenderStopsOnParsingError(t *testing.T) {
	fakeOpm(t, `
printf '%s' 'invalid JSON'
while :; do printf 'x'; done
`)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	got, err := catalog.Render(ctx, "catalog-reference")
	if got != nil || err == nil || !strings.Contains(err.Error(), "catalog record 1") {
		t.Fatalf("Render() = (%#v, %v), want parsing error", got, err)
	}
	if ctx.Err() != nil {
		t.Errorf("parser error did not stop the process before the deadline: %v", err)
	}
}

func TestRenderCancellation(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("CATALOG_TEST_READY", ready)
	fakeOpm(t, `
printf 'ready' > "$CATALOG_TEST_READY"
while :; do :; done
`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		inventory *catalog.Inventory
		err       error
	}
	done := make(chan result, 1)
	go func() {
		inventory, err := catalog.Render(ctx, "catalog-reference")
		done <- result{inventory: inventory, err: err}
	}()
	// Wait for the subprocess to start so this tests cancellation while Parse
	// is blocked on stdout, rather than only cancellation before cmd.Start.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("Render returned before cancellation: %v", got.err)
		case <-deadline.C:
			t.Fatal("subprocess did not start")
		case <-poll.C:
		}
	}
	cancel()
	select {
	case got := <-done:
		if got.inventory != nil || !errors.Is(got.err, context.Canceled) {
			t.Errorf("Render() = (%#v, %v), want wrapped cancellation", got.inventory, got.err)
		}
	case <-deadline.C:
		t.Fatal("cancellation did not unblock Render")
	}
}

func TestRenderBoundsStderr(t *testing.T) {
	fakeOpm(t, `
i=0
while [ "$i" -lt 2048 ]; do
  printf '%s' '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' >&2
  i=$((i+1))
done
printf '%s' 'end-of-stderr' >&2
exit 1
`)
	got, err := catalog.Render(t.Context(), "catalog-reference")
	if got != nil || err == nil {
		t.Fatalf("Render() = (%#v, %v), want error", got, err)
	}
	if !strings.Contains(err.Error(), "[stderr truncated]") || strings.Contains(err.Error(), "end-of-stderr") || len(err.Error()) > 66*1024 {
		t.Errorf("stderr was not bounded and marked as truncated (error length %d)", len(err.Error()))
	}
}
