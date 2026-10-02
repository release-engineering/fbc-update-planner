# AGENTS.md — fbc-update-planner

## What This Is

`plcc2fbc` fetches operator lifecycle data from Red Hat's Product Life Cycle Center (PLCC) API and converts it to File-Based Catalog (FBC) YAML blobs for OpenShift. Each PLCC product becomes an FBC package with versioned lifecycle phases (General Availability, End of Life, etc.).

## Tech Stack

- **Language:** Go (version in `go.mod`)
- **Dependencies:** `sigs.k8s.io/yaml` for YAML marshaling, `spf13/pflag` for CLI flag parsing, `github.com/avast/retry-go/v4` for HTTP retry logic
- **CI:** GitHub Actions on PRs to `main` — runs `make test` + `golangci-lint` (see `.github/workflows/tests.yaml`)
- **License:** Apache 2.0 (all `.go` files carry the header)

## Layout

```
cmd/plcc2fbc/main.go          CLI entry point — flag parsing, orchestration
cmd/plcc2fbc/version.go       Version/commit variables injected via ldflags
cmd/plcc2fbc/main_test.go     Tests for CLI (run function)
cmd/plcc-check/main.go       Reporting CLI — input loading and assessment orchestration
cmd/plcc-check/config.go     YAML selection and grouped skip-policy loading
cmd/plcc-check/artifacts.go  Staged artifact writing and publication
internal/plcccheck/check.go  Reporting assessment model and lifecycle gap classification
internal/plcccheck/versions.go  Catalog version normalization and coverage
internal/plcccheck/skip.go    Reporting exceptions that preserve assessment evidence
internal/plcccheck/report.go  Summary counts and complete text report
internal/plcccheck/slack.go   Bounded Slack payload generation (no posting)
internal/plcccheck/testdata/ Offline PLCC and rendered catalog assessment fixtures
pkg/catalog/catalog.go       Catalog coverage inventory and rendered JSON stream parsing
pkg/catalog/render.go        Context-aware opm render adapter for images and local catalogs
pkg/catalog/catalog_test.go  Tests for catalog parsing and metadata errors
pkg/catalog/render_test.go   Offline subprocess tests using a fake opm
pkg/plcc/plcc.go              Dataset API — source snapshot, working catalog, validation, filtering
pkg/plcc/catalog.go           PLCC data types, catalog operations, selection and copy helpers
pkg/plcc/client.go            PLCC API fetching, retries, and local JSON loading
pkg/plcc/findings.go          Structured validator metadata and per-product findings
pkg/plcc/legacy.go            Compatibility API for former selection and validation methods
pkg/plcc/validation.go        PLCC validator registry — per-product and catalog-level checks
pkg/plcc/plcc_test.go         Tests for the Dataset API
pkg/plcc/catalog_test.go      Tests for catalog types and operations
pkg/plcc/client_test.go       Tests for fetching and retries
pkg/plcc/legacy_test.go       Tests for legacy API compatibility
pkg/plcc/validation_test.go   Tests for PLCC validators
pkg/fbc/doc.go                Package documentation
pkg/fbc/types.go              Structured FBC types: MajorMinor, Date
pkg/fbc/fbc.go                FBC schema, Translate(), TranslateProduct()
pkg/fbc/fbc_test.go           Tests for FBC translation
pkg/fbc/conversion.go         Converter registry — PLCC→FBC field translation checks
pkg/fbc/conversion_test.go    Tests for converters
pkg/fbc/filter.go             Filter registry — output cleanup pipeline
pkg/fbc/filter_test.go        Tests for filters
pkg/fbc/writer.go             PackageWriter interface + JSON/YAML serializers
pkg/fbc/writer_test.go        Tests for writers
pkg/fbc/pipeline_test.go      Integration test — full pipeline vs reference output
pkg/fbc/testdata/             Test fixtures (plcc.json, reference-fbc.yaml, etc.)
pkg/report/result.go          Shared ValidationResult type + JSON-lines log writer
test/e2e/e2e_test.go          End-to-end tests — build binary, run against fixture, compare output
test/e2e/plcc_check_test.go   Reporting CLI tests — selection, catalog coverage, Slack, and artifacts
test/e2e/plcc_check_command_test.go  Reporting command subprocess tests and real opm input parity
test/e2e/catalog_test.go      Catalog reader integration tests using opm and local fixtures
test/e2e/testdata/            E2e test fixtures (plcc.json, reference YAMLs, untranslatable.json, plcc-check/)
docs/VALIDATION_RULES.md      Filter pipeline spec (read before touching filters)
docs/PLCC_API.md              Dataset API, ownership, validation, and compatibility
docs/PLCC_CHECK.md            Assessment API, evidence, coverage, and action policy
docs/FBC_SCHEMA.md            FBC output schema reference
docs/E2E_TESTS.md             E2e test architecture, test matrix, golden file workflow
docs/RELEASING.md             Release process and version injection reference
schema-examples/              Example PLCC + FBC schemas for reference
scripts/plcc-check-all.yaml   All-operator reporting configuration and skip groups
scripts/plcc-check-top.yaml   Top-operator selection and skip groups
scripts/top-operators         Legacy plain-text operator selection
.goreleaser.yaml              GoReleaser config for cross-platform binary builds
.github/workflows/tests.yaml  CI workflow — runs tests + lint on PRs to main
.github/workflows/release.yaml  Release workflow — runs GoReleaser on v* tag push
```

## Commands

```sh
make build              # → bin/plcc2fbc
make plcc-check         # → bin/plcc-check (PLCC and catalog reporting)
make test               # go test -v -count 1 ./...
make e2e                # go test -v -count 1 ./test/e2e/
make update-e2e         # regenerate e2e reference files from existing testdata/plcc.json
make update-e2e-source  # fetch fresh plcc.json from PLCC API + regenerate references
make update-e2e-plcc-check  # regenerate reporting golden files from local PLCC/catalog fixtures
make generate-fbc       # build + run against live PLCC API, write YAML + logs to fbc-samples/
```

No separate lint command — CI runs `golangci-lint` with defaults (no `.golangci.yaml`).

**Releasing:** Tag-triggered — push a `v*` tag to run GoReleaser via `.github/workflows/release.yaml`. See `docs/RELEASING.md` for the full workflow.

### CLI Flags

```
plcc2fbc [flags] <output-path>

-o, --output        Output format: json, json-pretty, or yaml (default: json)
-l, --log           Write validation/filtering report to a file (default: stderr)
-p, --package       Comma-separated package names to process (default: all)
-i, --input         Read PLCC JSON from a file instead of fetching from API
    --dump-plcc     Dump filtered PLCC JSON instead of generating FBC
    --permissive    Keep packages that fail PLCC validation instead of filtering them out
    --allow-missing Warn about missing -p packages instead of aborting
    --validators    Comma-separated validators to run: labels, or groups all/none/syntax/semantic/catalog (default: all)
    --list-validators  List available validators and exit
    --split         Write each package to <dir>/<package>/lifecycle.{json,yaml}; positional arg is a directory
```

## Architecture

### Catalog ingestion

`catalog.Parse(io.Reader)` reads an existing rendered JSON stream without an
external binary. `catalog.Render(context.Context, reference)` invokes `opm render`
and uses the same parser, inheriting registry authentication from the environment.
Both return a caller-owned `catalog.Inventory` keyed by package name, containing
bundle identities and original versions, lifecycle entry presence, and lifecycle
version names. Other schemas and unused fields are ignored. Consumed metadata
errors discard the entire inventory; lifecycle records without a package name are
ignored for compatibility. Version syntax checks, MAJOR.MINOR normalization,
deduplication, and PLCC comparison belong to callers. The reporting command uses
this package for catalog inspection.

### Data Flow

`internal/plcccheck.Assess` combines one PLCC source snapshot with an optional
catalog inventory to produce report facts and actions. It uses the Dataset's
selected validators and whole-product FBC translation with default filters.
Failures retain source identity. Typed issues track missing PLCC and catalog
content separately from the operator action: PLCC add, PLCC fix, OPERATOR add,
OPERATOR build, or OK. PLCC actions take precedence over catalog actions; no
bundles means OPERATOR add even if lifecycle data is present. Text and Slack's ACTION
column shows 📋 PLCCDATA, 📦 OPERATOR, or ✅. After assessment, `ApplySkips` can
replace recommendations with SKIPPED and attach a reason, preserving all evidence
and pipeline artifacts. Both tables show ➖ alone for skipped ACTION cells.
The SKIPPED table column contains the note. CSV lists group operators
under ✅ OK, 📋 PLCCDATA, 📦 OPERATOR, and ➖ SKIPPED; JSON retains all action labels.
PLCC status precedence is MISSING, DUPLICATE, INVALID, REGRESSED, INCOMPLETE, OK.
Text and Slack tables render INCOMPLETE as X/Y: required versions present in
source PLCC / distinct bundle MAJOR.MINOR versions, sharing the catalog denominator.
Summary counts and JSON use INCOMPLETE (`incomplete`) for both PLCC and catalog
version gaps.
Completeness checks all bundle versions; regression checks all shipped lifecycle
versions, including those without bundles. Catalog status is NO BUNDLES, MISSING,
OK, or X/Y. Summaries count operators by status and show one row per
operator (Action | Operator | PLCC | Catalog | Skipped); details group failure reasons
and issues by package, showing only the skip note for skipped operators.
Status counts and READY exclude skipped operators. Both summaries identify the
supplied catalog image or rendered input file, followed by a blank line, total
operators, skipped operators with an inline exclusion note, then READY as
fullyOK/nonSkipped before status counts. READY is a bold Slack header, omitted
without a catalog check, and displays 0/0 when no non-skipped operators remain.
JSON retains the fullyOK and nonSkipped fields. List headings explain each action group and show its count
over the total assessed operators, including any names omitted from Slack.
The text report includes the complete four CSV lists;
Slack uses larger, bold headers for Summary, Table, List, and Details.
The assessment retains filtered PLCC and translated FBC
for artifacts from the same pass. `cmd/plcc-check` orchestrates loading and
artifact writing; renderers share a `plcccheck.Report`. Both daily workflows build
and invoke `bin/plcc-check` directly, requesting Slack summary, table, and list.
Each workflow uses a separate YAML `--config` with optional `selected` names and
`skipped` groups (required `reason` and `operators`). Omitted selection means all;
empty selection is invalid. Skip rules never expand the assessed set. The legacy
positional operator-list file remains supported, mutually exclusive with `--config`.
The `table` section has one row per operator; `list` has CSV names grouped by action.
See `docs/PLCC_CHECK.md` for command flags and artifact semantics.

The existing `plcc2fbc` CLI still follows this legacy flow:

```
PLCC API (or -i file) → plcc.Fetch()/Load()
  → catalog.FilterByPackageNames()    # if -p flag set; returns PackagesNotFoundError on missing packages (--allow-missing downgrades to warning)
  → catalog.DropWithoutPackageName()  # otherwise: drop products without package names
  → catalog.SortByPackage()           # alphabetical
  → catalog.Validate()                # catalog-level PLCC validators (cross-product checks)
  → plcc.ValidateProduct()            # per-product PLCC validators (filter out failures; --permissive keeps them)
  → catalog.ExpandPackages()          # split comma-separated package names into separate products
  → catalog.SortByPackage()           # re-sort expanded products
  → writeFBC()                        # single-file mode (default):
      → fbc.Translate()              # batch translate all products, collect valid + failures
      → writer.Write()               # write all valid packages at once
  → writeSplitFBC()                  # --split mode:
      → fbc.TranslateProduct()       # per product: convert + filter (fail-fast)
      → writer.Write()               # write each package to <dir>/<package>/lifecycle.{json,yaml}

With --dump-plcc:
  → catalog.Dump()                  # write filtered PLCC JSON directly, skip FBC generation
```

### Three pipeline layers

1. **PLCC validators** (`pkg/plcc/validation.go`): data quality checks on raw `plcc.Product` values *before* FBC translation. By default they filter out failing packages; with `--permissive` they produce warnings only. Organized in two registries (`validatorRegistry` for per-product, `catalogValidatorRegistry` for cross-product) with labels (e.g. `REQ-DATE-03`, `CUSTOM-01`, `REQ-VAL-01`) and groups (`syntax`, `semantic`, `catalog`). Selectable via `--validators` flag.

2. **FBC converters** (`pkg/fbc/conversion.go`): type-checked field translation from `plcc.Version` to `fbc.Version`. Each converter validates one aspect and populates the corresponding output field. Organized in `converterRegistry` with labels (`FBC-VER-01` version name, `FBC-PHASE-01` phase timestamps, `FBC-OCP-01` OCP compatibility). Always run — cannot be disabled. If any converter returns errors, the entire package is rejected.

3. **FBC filter pipeline** (`pkg/fbc/filter.go`): output cleanup and invariant validation on translated `*fbc.Package` values. Organized in `filterRegistry` with per-filter labels: `FBC-MUTATE-01` (drop incomplete phases), `FBC-VAL-01`–`FBC-VAL-05` (structural invariants: versions exist, phases exist, dates non-nil, date ordering, phase contiguity). Labels are embedded in reason strings. See `docs/VALIDATION_RULES.md` for the full specification.

### Key Types

- `plcc.Catalog` / `plcc.Product` / `plcc.Version` / `plcc.Phase` — API-side types
- `plcc.PackagesNotFoundError` — custom error returned by `FilterByPackageNames` when `-p` packages are missing
- `plcc.Validator` — `func(Product) []string` — per-product validator callback
- `plcc.CatalogValidator` — `func([]Product) CatalogRejections` — cross-product validator
- `fbc.Package` / `fbc.Version` / `fbc.Phase` / `fbc.Platform` — output-side types
- `fbc.MajorMinor` — structured MAJOR.MINOR version (regex-validated, no leading zeros)
- `fbc.Date` — structured YYYY-MM-DD date; `*Date` fields use nil for absent dates
- `fbc.Converter` — `func(src plcc.Version, dst *Version) []error` — conversion check callback
- `fbc.Filter` — `func(*Package) []string` — output cleanup pipeline callback
- `fbc.PackageWriter` — interface for serializing packages (JSON, JSON-pretty, YAML)
- `report.ValidationResult` — structured JSON logged to stderr (or to a file via `-l`) for rejected/warned packages

### FBC Schema

Output blobs use schema `io.openshift.operators.lifecycles.v1alpha1`. See `docs/FBC_SCHEMA.md` for field details.

## Patterns to Follow

### Adding a PLCC validator

1. Write `func ValidateMyRule(p Product) []string` in `pkg/plcc/validation.go`
2. Add an entry to `validatorRegistry` with a label (e.g. `REQ-FOO-01`) and group (`syntax` or `semantic`)
3. For cross-product checks, use `CatalogValidator` signature and add to `catalogValidatorRegistry`
4. Add test in `pkg/plcc/validation_test.go` — table-driven, cover accept + reject paths

### Adding an FBC converter

1. Write `func ConvertMyField(src plcc.Version, dst *Version) []error` in `pkg/fbc/conversion.go`
2. Add an entry to `converterRegistry` with a label (e.g. `FBC-FOO-01`) and group `"converter"`. Embed the label in all error messages.
3. Add test in `pkg/fbc/conversion_test.go` — table-driven, cover valid + invalid inputs

### Adding an FBC output filter

1. Write `func FilterMyCleanup(p *Package) []string` in `pkg/fbc/filter.go`
2. Add an entry to `filterRegistry` with a label (e.g. `FBC-MUTATE-02` for mutations, `FBC-VAL-06` for invariants) and group (`"filter"` or `"invariant"`). Embed the label in all reason strings.
3. Add test in `pkg/fbc/filter_test.go` — table-driven
4. Read `docs/VALIDATION_RULES.md` first

### Writing tests

Three test tiers, each with its own scope:

1. **Unit tests** (`*_test.go` alongside source) — test individual functions. Run with `make test`.
2. **Integration tests** (`pkg/fbc/pipeline_test.go`) — compare full pipeline output against reference files in `pkg/fbc/testdata/`.
3. **E2E tests** (`test/e2e/e2e_test.go`) — build the binary and invoke it as a subprocess, verifying exit codes, file I/O, and CLI flag behavior against golden files in `test/e2e/testdata/`. Gated by `//go:build e2e` so `make test` excludes them; run with `make e2e` (passes `-tags=e2e`). See `docs/E2E_TESTS.md` for the test matrix and golden file update workflow.

- If your change alters valid output, update reference files to match (`make update-e2e` for e2e golden files)
- Standard library test assertions — no external assertion libraries

### Version format

Versions must match `^\d+\.\d+$` (MAJOR.MINOR only). This is checked by `ValidateVersionNames` in the PLCC validator layer.

### Timestamps

- PLCC API uses ISO8601 with milliseconds: `2025-11-11T00:00:00.000Z`
- FBC output uses `YYYY-MM-DD`
- `"N/A"` or empty timestamps translate to nil (omitted from output)

## Gotchas

- The CLI exits with code 1 for fatal errors, code 2 if no valid FBC blobs are produced, and code 3 if requested `-p` packages are not found (without `--allow-missing`) — all are intentional
- `FilterIncompletePhases` mutates the package in place (drops phases) — it never rejects
- All `.go` files must have the Apache 2.0 license header
- No `.golangci.yaml` — linter uses upstream defaults
- Design choice: `newPackage()` delegates to `translateVersion()` which iterates `converterRegistry` directly; any converter error (malformed version name, unparseable timestamps, invalid OCP format) rejects the entire package. The FBC type layer enforces schema invariants by construction, separate from PLCC validators which enforce data quality policy
- Logging model: structured `slog` logs always go to stdout (JSON handler). Validation/filtering reports (`report.LogResults`) default to stderr; `-l` redirects them to a file. `main()` prints a human-readable error to stderr for all non-zero exit codes; `run()` uses `slog.Error` only for exit-code-3 (per-package details on stdout)
- All structured logging uses `log/slog` (JSON handler) — the `log` package is not used
