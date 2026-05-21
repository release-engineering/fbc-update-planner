# PLCC Data Validation Rules

This document describes the rules that `plcc2fbc` uses to validate data from the Red Hat Product Life Cycle Checker (PLCC) API before generating FBC (File-Based Catalog) lifecycle blobs.

---

## Overview

After PLCC data is fetched and translated into FBC packages, each package passes through two stages:

1. **Pre-pipeline checks** (`TranslateAndValidate` in `pkg/fbc/fbc.go`): rules that **operate across all raw entries** (for invalid and duplicate detection).
2. **Filter pipeline** (`pkg/fbc/filter.go`): an ordered sequence of `Filter` callbacks that can mutate, validate, or reject **a single package**.

A package is emitted as an FBC blob only if it passes both stages.

The Filter pipeline is modular and easily extensible, see how in the ["Adding a New Filter" section](#adding-a-new-filter).


---
### Pre-Pipeline Checks

The Pre-Pipeline checks operate across products rather than within a single package and are meant to ensure that the Package name is present and that it maps to exactly one product.

If the same `package` value appears on multiple PLCC products, the package is marked **ambiguous** and rejected.

---
### Filter Pipeline

The pipeline is defined by `DefaultFilters()` in `pkg/fbc/filter.go`. Each filter has the signature:

```go
type Filter func(*Package) []string
```

A filter can **mutate** the package (e.g., drop incomplete phases), **validate** it (e.g., check version names), or both. Returning a non-empty `[]string` rejects the package — the strings describe why. Returning `nil` means the package passes that step.

Filters run in order, and the pipeline **short-circuits**: the first filter that returns reasons stops execution. This means earlier filters can prepare data for later ones (e.g., removing incomplete phases before validating continuity).

The default pipeline runs in this order:

| # | Function | Kind | Purpose |
|---|----------|------|---------|
| 1 | `FilterPointInTimePhases` | validate | Reject packages with misaligned point-in-time phases |
| 2 | `FilterIncompletePhases` | mutate | Drop phases where either date is empty |
| 3 | `ValidateHasVersions` | validate | Reject packages with no versions |
| 4 | `ValidateVersionNames` | validate | Reject packages with non-`MAJOR.MINOR` version names |
| 5 | `ValidatePhases` | validate | Reject packages with empty dates, end <= begin, or non-contiguous phases |
| 6 | `ValidateOCPCompatibility` | validate | Reject packages with non-`MAJOR.MINOR` OCP compatibility values |

#### Step 1: `FilterPointInTimePhases`

Phases are classified by their dates (after translation to FBC format, where unset dates become empty strings):

| Category | `startDate` | `endDate` | Handling |
|---|---|---|---|
| **N/A phase** | empty | empty | Ignored by this filter |
| **Complete phase** | set | set | Used as anchors |
| **Point-in-time phase** | one set, one empty | | Subject to alignment rules below |

Point-in-time phases are allowed only in two positions:

- **Before the first complete phase**: `startDate` is empty, and `endDate` exactly equals the first complete phase's `startDate`.
- **After the last complete phase**: `endDate` is empty, and `startDate` exactly equals the last complete phase's `endDate`.

Any misaligned point-in-time phase **rejects the entire package**. If no complete phases or no point-in-time phases exist, this filter passes silently.

#### Step 2: `FilterIncompletePhases`

Removes phases where either `startDate` or `endDate` is empty. This includes both N/A phases (both empty) and valid point-in-time phases that passed step 1.

This filter always returns `nil` — it mutates the package but never rejects it.

#### Step 3: `ValidateHasVersions`

Rejects the package if it has no versions.

#### Step 4: `ValidateVersionNames`

Each version `name` must match the regex `^\d+\.\d+$` (e.g., `4.12`, `1.0`). Versions with patch components, pre-release suffixes, or any other format cause rejection.

#### Step 5: `ValidatePhases`

For each version:

- There must be at least one phase (after filtering).
- Each phase must have non-empty `startDate` and `endDate` (should already be guaranteed by step 2, but flagged as an error if found).
- Each phase's `endDate` must be strictly after its `startDate`.
- Consecutive phases must be **contiguous**: the `startDate` of phase N must be exactly one day after the `endDate` of phase N-1 (e.g., if phase 1 ends `2024-06-30`, phase 2 must begin `2024-07-01`).

#### Step 6: `ValidateOCPCompatibility`

For each version with an `openshift` platform compatibility entry, every version string must match `^\d+\.\d+$`.

---

## Summary of filtering behavior

| Condition | Stage | Effect |
|---|---|---|
| Product has no `package` | Pre-pipeline | Silently skipped |
| Package maps to multiple products | Pre-pipeline | Package rejected |
| Misaligned point-in-time phase | Step 1 | Package rejected |
| Package has no versions | Step 3 | Package rejected |
| Version name not `MAJOR.MINOR` | Step 4 | Package rejected |
| No phases in a version | Step 5 | Package rejected |
| Phase end not after begin | Step 5 | Package rejected |
| Phase gap or overlap (not contiguous) | Step 5 | Package rejected |
| OCP compatibility entry not `MAJOR.MINOR` | Step 6 | Package rejected |

All validation failures are logged as structured JSON to stderr with the `packageName`, `valid: false`, and a `reasons` array describing each issue.

---


## Adding a New Filter

All filters live in `pkg/fbc/filter.go`. To add a new filter:

1. **Write a new function** with signature `func(p *Package) []string`.
    * Return a list of reason strings to reject the package or `nil` to accept it.
    * Mutate the package `p` as needed (e.g., drop or rewrite data).

2. **Add it to `DefaultFilters()`** at the appropriate position.
    * Order matters: mutating filters (that prepare data) should run before validators (that check data).
    * The pipeline short-circuits on the first rejection, so place stricter checks earlier if they make later checks meaningless.

3. **Add a test** in `pkg/fbc/filter_test.go`.
    * one test function per filter, with a few table-driven cases covering the accept and reject paths.

### Example skeleton:

```go
func ValidateMyRule(p *Package) []string {
    var reasons []string
    for _, v := range p.Versions {
        if !someCheck(v) {
            reasons = append(reasons, fmt.Sprintf("version %q: failed my rule", v.Name))
        }
    }
    return reasons
}
```

Then in `DefaultFilters()`:

```go
return []Filter{
    FilterPointInTimePhases,
    FilterIncompletePhases,
    ValidateHasVersions,
    ValidateVersionNames,
    ValidatePhases,
    ValidateOCPCompatibility,
    ValidateMyRule, // added
}
```

Note: `DefaultFilters()` returns a fresh slice each time, so callers can safely append or reorder filters for custom pipelines without affecting the default.
