# PLCC Data Validation Rules

This document describes the rules that `plcc2fbc` uses to validate data from the Red Hat Product Life Cycle Checker (PLCC) API before generating FBC (File-Based Catalog) lifecycle blobs.

---

## Overview

After PLCC data is fetched, each product passes through two stages:

1. **PLCC-level validation** (`pkg/plcc/validation.go`): validators that check raw PLCC data quality (tier, release cadence, phase completeness, dates, duplicates). By default these produce **warnings**; with `--strict` they filter out failing packages.
2. **Filter pipeline** (`pkg/fbc/filter.go`): an ordered sequence of `Filter` callbacks that clean the translated FBC output (e.g., drop incomplete phases).

The Filter pipeline is modular and easily extensible, see how in the ["Adding a New Filter" section](#adding-a-new-filter).

---
### Filter Pipeline

The pipeline is defined by `DefaultFilters()` in `pkg/fbc/filter.go`. Each filter has the signature:

```go
type Filter func(*Package) []string
```

Filters **mutate** packages to produce clean FBC output (e.g., drop incomplete phases). They do not perform validation — data quality checks belong in the PLCC validators. Returning a non-empty `[]string` rejects the package; returning `nil` means the package passes.

Filters run in order, and the pipeline **short-circuits**: the first filter that returns reasons stops execution.

The default pipeline:

| # | Function | Kind | Purpose |
|---|----------|------|---------|
| 1 | `FilterIncompletePhases` | mutate | Drop phases where either date is empty |

#### `FilterIncompletePhases`

Removes phases where either `startDate` or `endDate` is empty. This includes N/A phases (both empty) and point-in-time phases (one set, one empty).

This filter always returns `nil` — it mutates the package but never rejects it.

---

## Summary of filtering behavior

| Condition | Stage | Effect |
|---|---|---|
| Product has no `package` | PLCC filtering | Silently skipped |
| Package maps to multiple products | PLCC validation | Warning (rejected with `--strict`) |
| Phase with empty start or end date | Filter pipeline | Phase silently removed |

---


---
### PLCC-Level Validation

Before the FBC filter pipeline runs, `main.go` calls PLCC-level validators on each raw `plcc.Product`. These validators live in `pkg/plcc/validation.go` alongside the data types they check. They produce **warnings** (logged as structured JSON to stderr) but do not block packages from FBC output. Use `--strict` to promote warnings to errors and filter out failing packages. Use `--validators` to select which validators to run (by label or group), and `--list-validators` to see available options.

Validators are split into two groups via `SyntaxValidators()` (data format/structure) and `SemanticValidators()` (business/lifecycle rules). `DefaultValidators()` composes both. Catalog-level checks run via `catalog.Validate()`.

#### Syntax Validators

| # | Function | Label | Purpose |
|---|----------|-------|---------|
| 1 | `ValidateIsOperator` | CUSTOM-01 | Product must have package name and be flagged as operator |
| 2 | `ValidateHasVersions` | CUSTOM-02 | Product must have ≥1 version |
| 3 | `ValidateDatesStatic` | REQ-DATE-02 | Dates must be static values (checks `_format` field) |
| 4 | `ValidateDatesClean` | REQ-DATE-03 | Non-empty, non-N/A dates must cleanly parse |
| 5 | `ValidatePointInTimePhases` | REQ-DATE-04 | Point-in-time phases must align (+1 day) with adjacent phases |
| 6 | `ValidateDatesContiguity` | REQ-DATE-04 | Consecutive phases must start 1 day after previous ends |
| 7 | `ValidatePhaseEndAfterStart` | CUSTOM-03 | Phase end date must be after start date |
| 8 | `ValidateVersionNames` | REQ-VER-01 | Version names must match `MAJOR.MINOR` |
| 9 | `ValidateOCPFormat` | REQ-FIELD-02 | OCP compatibility on aligned versions must match `MAJOR.MINOR` |
| 10 | `ValidateOCPFormatAll` | CUSTOM-04 | OCP compatibility format on non-aligned versions |

#### Semantic Validators

| # | Function | Label | Purpose |
|---|----------|-------|---------|
| 1 | `ValidateReleaseCadence` | REQ-TIER-ALL-01 | Operators must have release cadence specified |
| 2 | `ValidateTierSelected` | REQ-TIER-ALL-02 | Operator versions must have lifecycle tier selected |
| 3 | `ValidatePlatformAlignedPhases` | REQ-TIER-PA-01 | Aligned: Full Support, Maintenance, EUS 1/2/3 with parseable dates |
| 4 | `ValidatePlatformAlignedOCP` | REQ-TIER-PA-02 | Aligned: OCP compatibility must be specified |
| 5 | `ValidatePlatformAgnosticPhases` | REQ-TIER-AG-01 | Agnostic: Full Support and Maintenance with parseable dates |
| 6 | `ValidatePlatformAgnosticEUSPhases` | REQ-TIER-AG-03 | EUS-aligned agnostic: all 3 EUS terms with parseable dates, or none |
| 7 | `ValidatePlatformAgnosticEUSOCP` | REQ-TIER-AG-04 | EUS-aligned agnostic: OCP compatibility must be specified |
| 8 | `ValidateRollingStreamPhases` | REQ-TIER-RS-01 | Rolling: Full Support with parseable dates |
| 9 | `ValidateRollingStreamForbiddenPhases` | REQ-TIER-RS-02 | Rolling: must not include Maintenance or EUS phases |

#### Catalog-Level Validators

| # | Function | Label | Purpose |
|---|----------|-------|---------|
| 1 | `ValidateNoDuplicates` | REQ-VAL-01 | No package name appears in multiple products |

---

## Adding a New Filter

Filters live in `pkg/fbc/filter.go` and are for **output cleanup only** — mutating or dropping data to produce clean FBC blobs. Data quality validation belongs in the PLCC validators (`pkg/plcc/validation.go`).

1. **Write a new function** with signature `func(p *Package) []string`.
    * Mutate the package `p` as needed (e.g., drop or rewrite data).
    * Return `nil` to accept or a list of reason strings to reject.

2. **Add it to `DefaultFilters()`** at the appropriate position.

3. **Add a test** in `pkg/fbc/filter_test.go`.

Note: `DefaultFilters()` returns a fresh slice each time, so callers can safely append or reorder filters for custom pipelines without affecting the default.
