# AGENTS.md — fbc-update-planner

## What This Is

`plcc2fbc` fetches operator lifecycle data from Red Hat's Product Life Cycle Center (PLCC) API and converts it to File-Based Catalog (FBC) YAML blobs for OpenShift. Each PLCC product becomes an FBC package with versioned lifecycle phases (General Availability, End of Life, etc.).

## Tech Stack

- **Language:** Go (version in `go.mod`)
- **Dependencies:** `sigs.k8s.io/yaml` for YAML marshaling, `spf13/pflag` for CLI flag parsing
- **CI:** GitHub Actions on PRs to `main` — runs `make test` + `golangci-lint` (see `.github/workflows/tests.yaml`)
- **License:** Apache 2.0 (all `.go` files carry the header)

## Layout

```
cmd/plcc2fbc/main.go          CLI entry point — flag parsing, orchestration
cmd/plcc2fbc/main_test.go     Tests for CLI (run function)
pkg/plcc/plcc.go              PLCC API client, data types, filtering, sorting
pkg/plcc/validation.go        PLCC validator registry — per-product and catalog-level checks
pkg/plcc/plcc_test.go         Tests for PLCC package
pkg/plcc/validation_test.go   Tests for PLCC validators
pkg/fbc/doc.go                Package documentation
pkg/fbc/types.go              Structured FBC types: MajorMinor, Date
pkg/fbc/fbc.go                FBC schema, GenerateFBC(), Translate(), TranslateProduct()
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
docs/VALIDATION_RULES.md      Filter pipeline spec (read before touching filters)
docs/FBC_SCHEMA.md            FBC output schema reference
schema-examples/              Example PLCC + FBC schemas for reference
fbc-samples/                  Generated FBC snapshots (YAML, logs, validation logs)
scripts/plcc-check.sh            Batch runner — runs plcc2fbc against a list of operators, summarizes results
scripts/top-operators             Default operator list for plcc-check.sh
.github/workflows/tests.yaml  CI workflow definition
```

## Commands

```sh
make build          # → bin/plcc2fbc
make test           # go test -v -count 1 ./...
make generate-fbc   # build + run against live PLCC API, write YAML + logs to fbc-samples/
```

No separate lint command — CI runs `golangci-lint` with defaults (no `.golangci.yaml`).

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
    --validators    Comma-separated validators to run: labels, or groups all/syntax/semantic/catalog (default: all)
    --list-validators  List available validators and exit
    --split         Write each package to <dir>/<package>/lifecycle.{json,yaml}; positional arg is a directory
```

## Architecture

### Data Flow

```
PLCC API (or -i file) → plcc.Fetch()/Load()
  → catalog.FilterByPackageNames()  # if -p flag set; returns PackagesNotFoundError on missing packages (--allow-missing downgrades to warning)
  → catalog.FilterPackages()        # otherwise: drop products without package names
  → catalog.SortByPackage()         # alphabetical
  → catalog.Validate()              # catalog-level PLCC validators (cross-product checks)
  → plcc.ValidateProduct()          # per-product PLCC validators (filter out failures; --permissive keeps them)
  → fbc.GenerateFBC()               # translate (1 Product → N FBC Packages for comma-separated names) + filter + write via PackageWriter

With --split:
  → fbc.TranslateProduct()          # per product per package name: convert + filter, fail-fast on first error
  → writer.Write()                  # write each package to <dir>/<package>/lifecycle.{json,yaml}

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

- Test data lives in `pkg/fbc/testdata/` (plcc.json, reference-fbc.yaml, reference-fbc.json, reference-fbc-pretty.json)
- `pipeline_test.go` is the integration test — compares full pipeline output against reference files
- If your change alters valid output, update reference files to match
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
- `fbc-samples/` contains committed generated files — update via `make generate-fbc`, not by hand
- No `.golangci.yaml` — linter uses upstream defaults
- Design choice: `newPackage()` delegates to `translateVersion()` which runs the converter registry (`DefaultConverters()`); any converter error (malformed version name, unparseable timestamps, invalid OCP format) rejects the entire package. The FBC type layer enforces schema invariants by construction, separate from PLCC validators which enforce data quality policy
- Logging model: structured `slog` logs always go to stdout (JSON handler). Validation/filtering reports (`report.LogResults`, `fbc.GenerateFBC` logOutput) default to stderr; `-l` redirects them to a file. `main()` prints a human-readable error to stderr for all non-zero exit codes; `run()` uses `slog.Error` only for exit-code-3 (per-package details on stdout)
- All structured logging uses `log/slog` (JSON handler) — the `log` package is not used
