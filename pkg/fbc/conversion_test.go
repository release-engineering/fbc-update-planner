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
	"testing"

	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

func TestConvertVersionName(t *testing.T) {
	tests := []struct {
		name    string
		version plcc.Version
		want    string
		wantErr bool
	}{
		{
			name:    "valid",
			version: plcc.Version{Name: "4.12"},
			want:    "4.12",
		},
		{
			name:    "invalid",
			version: plcc.Version{Name: "not-semver"},
			wantErr: true,
		},
		{
			name:    "leading zero",
			version: plcc.Version{Name: "01.2"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &Version{}
			errs := ConvertVersionName(tt.version, dst)
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected errors, got nil")
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if dst.Name.String() != tt.want {
				t.Errorf("Name = %q, want %q", dst.Name, tt.want)
			}
		})
	}
}

func TestConvertPhases(t *testing.T) {
	tests := []struct {
		name       string
		version    plcc.Version
		wantCount  int
		wantErr    bool
		wantNilDat bool
	}{
		{
			name: "valid phases",
			version: plcc.Version{Phases: []plcc.Phase{
				{Name: "Full support", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-06-30T00:00:00.000Z"},
				{Name: "Maintenance", StartDate: "2025-07-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"},
			}},
			wantCount: 2,
		},
		{
			name: "N/A timestamp becomes nil date",
			version: plcc.Version{Phases: []plcc.Phase{
				{Name: "GA", StartDate: "N/A", EndDate: "2025-01-01T00:00:00.000Z"},
			}},
			wantCount:  1,
			wantNilDat: true,
		},
		{
			name: "empty timestamp becomes nil date",
			version: plcc.Version{Phases: []plcc.Phase{
				{Name: "GA", StartDate: "", EndDate: "2025-01-01T00:00:00.000Z"},
			}},
			wantCount:  1,
			wantNilDat: true,
		},
		{
			name: "unparseable timestamp",
			version: plcc.Version{Phases: []plcc.Phase{
				{Name: "GA", StartDate: "not-a-date", EndDate: "2025-01-01T00:00:00.000Z"},
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &Version{}
			errs := ConvertPhases(tt.version, dst)
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected errors, got nil")
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if len(dst.Phases) != tt.wantCount {
				t.Fatalf("got %d phases, want %d", len(dst.Phases), tt.wantCount)
			}
			if tt.wantNilDat && dst.Phases[0].StartDate != nil {
				t.Errorf("expected nil StartDate, got %v", dst.Phases[0].StartDate)
			}
		})
	}
}

func TestConvertOCPCompatibility(t *testing.T) {
	tests := []struct {
		name      string
		version   plcc.Version
		wantCount int
		wantErr   bool
	}{
		{
			name:      "valid single",
			version:   plcc.Version{OpenShiftCompatibility: "4.12"},
			wantCount: 1,
		},
		{
			name:      "valid multiple",
			version:   plcc.Version{OpenShiftCompatibility: "4.12, 4.13"},
			wantCount: 2,
		},
		{
			name:    "N/A ignored",
			version: plcc.Version{OpenShiftCompatibility: "N/A"},
		},
		{
			name:    "empty ignored",
			version: plcc.Version{OpenShiftCompatibility: ""},
		},
		{
			name:    "invalid version",
			version: plcc.Version{OpenShiftCompatibility: "4.12, bad-version"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := &Version{}
			errs := ConvertOCPCompatibility(tt.version, dst)
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected errors, got nil")
				}
				if len(dst.PlatformCompatibility) > 0 {
					t.Error("expected no platform compatibility on error")
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if tt.wantCount == 0 {
				if len(dst.PlatformCompatibility) != 0 {
					t.Errorf("expected no platform compatibility, got %d", len(dst.PlatformCompatibility))
				}
				return
			}
			if len(dst.PlatformCompatibility) != 1 {
				t.Fatalf("expected 1 platform, got %d", len(dst.PlatformCompatibility))
			}
			if got := len(dst.PlatformCompatibility[0].Versions); got != tt.wantCount {
				t.Errorf("got %d OCP versions, want %d", got, tt.wantCount)
			}
		})
	}
}

func TestDefaultConverters(t *testing.T) {
	converters := DefaultConverters()
	if len(converters) != 3 {
		t.Errorf("got %d converters, want 3", len(converters))
	}
}

