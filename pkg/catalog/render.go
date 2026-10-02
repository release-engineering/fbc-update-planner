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

package catalog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Render invokes opm render for an image reference or local catalog directory
// and parses its JSON stream. It looks up opm in PATH and inherits the caller's
// environment, including registry authentication. The caller controls timeouts
// through ctx. Both rendering and parsing must succeed to return an inventory.
func Render(ctx context.Context, reference string) (*Inventory, error) {
	if strings.TrimSpace(reference) == "" {
		return nil, fmt.Errorf("catalog reference must be nonempty")
	}
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "opm", "render", "--output=json", "--", reference)
	// Bound waits for inherited stderr pipes if an opm child outlives it.
	cmd.WaitDelay = time.Second
	var stderr stderrBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opm render %q: %w", reference, err)
	}
	defer func() { _ = stdout.Close() }()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("opm render %q: %w", reference, err)
	}
	// Closing stdout also unblocks Parse if a child inherited the pipe and
	// keeps it open after the command itself has been killed on cancellation.
	stop := context.AfterFunc(commandCtx, func() { _ = stdout.Close() })
	defer stop()
	inventory, parseErr := Parse(stdout)
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := errors.Join(parseErr, waitErr, ctx.Err()); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			detail = "\nstderr: " + detail
		}
		return nil, fmt.Errorf("opm render %q: %w%s", reference, err, detail)
	}
	return inventory, nil
}

// stderrBuffer retains the first 64 KiB while continuing to drain stderr.
type stderrBuffer struct {
	data      []byte
	truncated bool
}

func (b *stderrBuffer) Write(p []byte) (int, error) {
	n := min(len(p), 64*1024-len(b.data))
	b.data = append(b.data, p[:n]...)
	if n < len(p) {
		b.truncated = true
	}
	return len(p), nil
}

func (b *stderrBuffer) String() string {
	if b.truncated {
		return string(b.data) + "\n[stderr truncated]"
	}
	return string(b.data)
}
