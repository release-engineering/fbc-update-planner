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

// Package catalog reads bundle and lifecycle coverage metadata from operator
// catalogs. Use [Parse] for an existing JSON stream, or [Render] to invoke opm
// for a catalog image or local catalog directory. Parse requires no external
// binary; Render inherits opm's registry authentication environment.
//
// An [Inventory] contains bundle names and original versions, lifecycle entry
// presence, and lifecycle version names. It includes bundle-only and
// lifecycle-only packages, without requiring an olm.package catalog object.
// Other schemas and unused fields, including lifecycle phases and platform
// compatibility, are ignored. This package does not validate complete catalogs
// or lifecycle contents. Required coverage fields are checked for their JSON
// types and nonempty values; an error yields no partial inventory. For
// compatibility, lifecycle entries with absent, null, or empty package names
// are ignored.
//
// Versions remain as recorded, with duplicates and encounter order preserved.
// Callers own the inventory and are responsible for version syntax checks,
// MAJOR.MINOR normalization, deduplication, comparison with PLCC, and reporting.
package catalog
