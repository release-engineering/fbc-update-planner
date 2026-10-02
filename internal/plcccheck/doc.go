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

// Package plcccheck assesses PLCC completeness and catalog lifecycle coverage for the
// reporting tool. It owns reporting policy and text, JSON, and Slack report
// models. Fetching, rendering catalogs, and writing artifacts belong to callers.
//
// Assess takes one source snapshot and constructs a PLCC Dataset with the run's
// selected validators. It retains all selected source identities, validates
// whole products, and translates accepted products with the mandatory FBC
// filters. Catalog presence is a version coverage check, not a validation of
// the lifecycle content already shipped in the catalog.
//
// Actions are operator-level recommendations: PLCC add, PLCC fix, OPERATOR add,
// OPERATOR build, or OK.
// PLCC completeness checks all bundle versions, including those already covered
// by catalog lifecycle data. Any shipped lifecycle version absent from current
// PLCC is reported as a regression, even when no bundle requires it. Findings
// retain each missing item independently of status and action precedence.
//
// Reports consume the same package assessment for summary counts and the
// Action/Operator/PLCC/Catalog table, then group every issue and failure reason
// under its package. Without a catalog, OK describes PLCC health only.
// See docs/PLCC_CHECK.md for the report contract and precedence rules.
package plcccheck
