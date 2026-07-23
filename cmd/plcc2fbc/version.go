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
	"runtime/debug"
	"strings"
)

// version is set at build time via -ldflags "-X main.version=..."
// When not set (e.g. go install), it defaults to "(devel)" and
// versionString falls back to runtime/debug.ReadBuildInfo.
var version = "(devel)"

// versionString returns the build version. If version was injected via
// ldflags it is returned directly. Otherwise, VCS metadata embedded by
// the Go toolchain is used as a fallback.
func versionString() string {
	if version != "(devel)" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}

	var vcs, rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs":
			vcs = s.Value
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}

	if vcs == "" || rev == "" {
		return version
	}

	// Shorten the revision hash to 12 characters, matching
	// git describe --always output length.
	if len(rev) > 12 {
		rev = rev[:12]
	}

	var b strings.Builder
	b.WriteString(rev)
	b.WriteString(dirty)
	return b.String()
}
