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
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var majorMinorRegexp = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// MajorMinor represents a version string in MAJOR.MINOR format.
type MajorMinor struct {
	Major uint64
	Minor uint64
}

// ParseMajorMinor parses a "MAJOR.MINOR" string into a MajorMinor value.
// Leading zeros are rejected (e.g., "01.2" is invalid).
func ParseMajorMinor(s string) (MajorMinor, error) {
	// matches is always nil (no match) or [fullMatch, major, minor] — the
	// regex has exactly 2 capture groups. TestMajorMinorRegexpGroups verifies this.
	matches := majorMinorRegexp.FindStringSubmatch(s)
	if len(matches) == 0 {
		return MajorMinor{}, fmt.Errorf("invalid version %q; expected <major>.<minor>", s)
	}
	major, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return MajorMinor{}, fmt.Errorf("invalid major version %q: %w", matches[1], err)
	}
	minor, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return MajorMinor{}, fmt.Errorf("invalid minor version %q: %w", matches[2], err)
	}
	return MajorMinor{Major: major, Minor: minor}, nil
}

func (m MajorMinor) String() string {
	return fmt.Sprintf("%d.%d", m.Major, m.Minor)
}

// Compare returns a negative value if m < other, zero if equal, positive if m > other.
func (m MajorMinor) Compare(other MajorMinor) int {
	if m.Major != other.Major {
		if m.Major < other.Major {
			return -1
		}
		return 1
	}
	if m.Minor != other.Minor {
		if m.Minor < other.Minor {
			return -1
		}
		return 1
	}
	return 0
}

func (m MajorMinor) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

func (m *MajorMinor) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseMajorMinor(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

const dateLayout = "2006-01-02"

// Date represents a calendar date in YYYY-MM-DD format.
type Date struct {
	t time.Time
}

// NewDate constructs a Date from year, month, and day components.
func NewDate(year int, month time.Month, day int) Date {
	return Date{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses a "YYYY-MM-DD" string into a Date value.
func ParseDate(s string) (*Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q; expected YYYY-MM-DD: %w", s, err)
	}
	return &Date{t: t}, nil
}

func (d Date) String() string {
	return d.t.Format(dateLayout)
}

func (d Date) IsZero() bool {
	return d.t.IsZero()
}

// Compare returns a negative value if d < other, zero if equal, positive if d > other.
func (d Date) Compare(other Date) int {
	return d.t.Compare(other.t)
}

func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d Date) NextDay() Date {
	return Date{t: d.t.AddDate(0, 0, 1)}
}

func (d *Date) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = *parsed
	return nil
}
