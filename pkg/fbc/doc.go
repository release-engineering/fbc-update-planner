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

// Package fbc translates PLCC product lifecycle data into File-Based Catalog (FBC)
// entries using the io.openshift.operators.lifecycles.v1alpha1 schema.
//
// Translation proceeds in two phases: a converter pipeline ([DefaultConverters])
// validates and translates each plcc.Version field into the corresponding
// fbc.Version field, then a filter pipeline ([DefaultFilters]) cleans the output
// (e.g., dropping incomplete phases).
//
// [TranslateProduct] converts a single PLCC product through both phases.
// [Translate] applies [TranslateProduct] to a batch of products. [GenerateFBC]
// is a convenience wrapper that combines translation with I/O handling.
// A [PackageWriter] serializes packages to a given format; use [NewPackageWriter]
// to obtain one.
package fbc
