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
	"testing"
	"time"
)

func mustParseMajorMinor(t *testing.T, s string) MajorMinor {
	t.Helper()
	mm, err := ParseMajorMinor(s)
	if err != nil {
		t.Fatalf("ParseMajorMinor(%q): %v", s, err)
	}
	return *mm
}

func mustParseDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return *d
}

func datePtr(t *testing.T, s string) *Date {
	t.Helper()
	d := mustParseDate(t, s)
	return &d
}

func TestParseMajorMinor(t *testing.T) {
	valid := []struct {
		input string
		major uint64
		minor uint64
	}{
		{"0.0", 0, 0},
		{"0.1", 0, 1},
		{"1.0", 1, 0},
		{"1.2", 1, 2},
		{"4.12", 4, 12},
		{"10.20", 10, 20},
	}
	for _, tt := range valid {
		t.Run(tt.input, func(t *testing.T) {
			mm, err := ParseMajorMinor(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mm.Major != tt.major || mm.Minor != tt.minor {
				t.Errorf("got %d.%d, want %d.%d", mm.Major, mm.Minor, tt.major, tt.minor)
			}
		})
	}

	invalid := []string{
		"",
		"1",
		"1.2.3",
		"a.b",
		"1.x",
		"-1.2",
		"01.2",
		"1.02",
		".1",
		"1.",
		"1.2 ",
		" 1.2",
		"v1.2",
	}
	for _, s := range invalid {
		t.Run("invalid_"+s, func(t *testing.T) {
			_, err := ParseMajorMinor(s)
			if err == nil {
				t.Fatalf("expected error for %q", s)
			}
		})
	}
}

func TestMajorMinorString(t *testing.T) {
	mm := MajorMinor{Major: 4, Minor: 12}
	if got := mm.String(); got != "4.12" {
		t.Errorf("String() = %q, want %q", got, "4.12")
	}
}

func TestMajorMinorCompare(t *testing.T) {
	tests := []struct {
		a, b     string
		wantSign int
	}{
		{"1.0", "2.0", -1},
		{"2.0", "1.0", 1},
		{"4.12", "4.13", -1},
		{"4.12", "4.12", 0},
		{"0.0", "0.1", -1},
		{"10.0", "2.0", 1},
	}
	for _, tt := range tests {
		a := mustParseMajorMinor(t, tt.a)
		b := mustParseMajorMinor(t, tt.b)
		got := a.Compare(b)
		switch {
		case tt.wantSign < 0 && got >= 0:
			t.Errorf("Compare(%s, %s) = %d, want < 0", tt.a, tt.b, got)
		case tt.wantSign > 0 && got <= 0:
			t.Errorf("Compare(%s, %s) = %d, want > 0", tt.a, tt.b, got)
		case tt.wantSign == 0 && got != 0:
			t.Errorf("Compare(%s, %s) = %d, want 0", tt.a, tt.b, got)
		}
	}
}

func TestMajorMinorJSON(t *testing.T) {
	mm := mustParseMajorMinor(t, "4.12")
	data, err := json.Marshal(mm)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != `"4.12"` {
		t.Errorf("MarshalJSON = %s, want %q", data, "4.12")
	}

	var roundTrip MajorMinor
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if roundTrip != mm {
		t.Errorf("round-trip = %v, want %v", roundTrip, mm)
	}
}

func TestMajorMinorUnmarshalJSONInvalid(t *testing.T) {
	var mm MajorMinor
	if err := json.Unmarshal([]byte(`"not-valid"`), &mm); err == nil {
		t.Fatal("expected error for invalid version string")
	}
	if err := json.Unmarshal([]byte(`123`), &mm); err == nil {
		t.Fatal("expected error for non-string JSON")
	}
}

func TestParseDate(t *testing.T) {
	valid := []struct {
		input string
		year  int
		month time.Month
		day   int
	}{
		{"2025-01-15", 2025, time.January, 15},
		{"2000-12-31", 2000, time.December, 31},
	}
	for _, tt := range valid {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDate(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := d.String()
			if got != tt.input {
				t.Errorf("String() = %q, want %q", got, tt.input)
			}
		})
	}

	invalid := []string{
		"2025/01/15",
		"Jan 15 2025",
		"N/A",
		"2025-1-1",
		"2025-01-15T00:00:00Z",
	}
	for _, s := range invalid {
		t.Run("invalid_"+s, func(t *testing.T) {
			_, err := ParseDate(s)
			if err == nil {
				t.Fatalf("expected error for %q", s)
			}
		})
	}
}

func TestDateString(t *testing.T) {
	d := NewDate(2025, time.March, 5)
	if got := d.String(); got != "2025-03-05" {
		t.Errorf("String() = %q, want %q", got, "2025-03-05")
	}
}

func TestDateJSON(t *testing.T) {
	d := mustParseDate(t, "2025-06-30")
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != `"2025-06-30"` {
		t.Errorf("MarshalJSON = %s, want %q", data, "2025-06-30")
	}

	var roundTrip Date
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if roundTrip.String() != d.String() {
		t.Errorf("round-trip = %v, want %v", roundTrip, d)
	}
}

func TestDatePtrJSON(t *testing.T) {
	d := datePtr(t, "2025-06-30")
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != `"2025-06-30"` {
		t.Errorf("MarshalJSON = %s, want %q", data, "2025-06-30")
	}

	type wrapper struct {
		D *Date `json:"d"`
	}
	w := wrapper{D: nil}
	data, err = json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON nil: %v", err)
	}
	if string(data) != `{"d":null}` {
		t.Errorf("nil *Date marshals as %s, want null", data)
	}
}

func TestDateUnmarshalJSONInvalid(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`"not-a-date"`), &d); err == nil {
		t.Fatal("expected error for invalid date string")
	}
	if err := json.Unmarshal([]byte(`123`), &d); err == nil {
		t.Fatal("expected error for non-string JSON")
	}
}
