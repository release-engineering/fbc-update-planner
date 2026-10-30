//go:build e2e

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

package e2e

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
)

func TestCatalogRenderInventory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	got, err := catalog.Render(ctx, "testdata/catalog-fbc-versions")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]catalog.Package{
		"aws-efs-csi-driver-operator": {HasLifecycle: true, LifecycleVersions: []string{"1.0"}},
		"cli-manager": {
			Bundles: []catalog.Bundle{
				{Name: "cli-manager.v0.1.0", Version: "0.1.0"},
				{Name: "cli-manager.v0.2.0", Version: "0.2.0"},
			},
			HasLifecycle: true, LifecycleVersions: []string{"0.1"},
		},
		"operator-full": {
			Bundles: []catalog.Bundle{
				{Name: "operator-full.v1.0.1", Version: "1.0.1"},
				{Name: "operator-full.v1.1.0", Version: "1.1.0"},
			},
			HasLifecycle: true, LifecycleVersions: []string{"1.0", "1.1"},
		},
		"operator-missing": {Bundles: []catalog.Bundle{{Name: "operator-missing.v1.0.0", Version: "1.0.0"}}},
		"operator-partial": {
			Bundles: []catalog.Bundle{
				{Name: "operator-partial.v1.0.0", Version: "1.0.0"},
				{Name: "operator-partial.v1.1.0", Version: "1.1.0"},
				{Name: "operator-partial.v1.2.0", Version: "1.2.0"},
			},
			HasLifecycle: true, LifecycleVersions: []string{"1.0", "1.1"},
		},
	}
	if !reflect.DeepEqual(got.Packages, want) {
		t.Errorf("inventory = %#v, want %#v", got.Packages, want)
	}
}

func TestCatalogRenderIgnoresEmptyLifecyclePackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	got, err := catalog.Render(ctx, "testdata/catalog-fbc-empty-package")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 0 {
		t.Errorf("inventory = %#v, want empty", got.Packages)
	}
}
