# End-to-End Tests

End-to-end tests exercise the full `plcc2fbc` CLI pipeline — flag parsing, PLCC loading, validation, FBC translation, filtering, and file I/O — by building the binary and invoking it as a subprocess against golden reference files. `test/e2e/plcc_check_test.go` and `plcc_check_command_test.go` cover the Go reporting command, `plcc-check`.

---

## Architecture

`TestMain` compiles `plcc2fbc` and `plcc-check` from source into a temporary directory once per test run. Tests invoke them through `runBinary` and `runPlccCheck`, which capture stdout, stderr, and the exit code. Output is compared byte-for-byte against reference files in `test/e2e/testdata/`.

`runPlccCheck` supplies stable GitHub workflow environment variables for Slack artifact links. Reporting tests use local PLCC snapshots or a stalled local proxy for cancellation checks. No reporting tests contact the live API.

The e2e package uses a `//go:build e2e` build tag so that `go test ./...` (i.e. `make test`) does not include it. Run with `make e2e` (which passes `-tags=e2e`) to execute the suite.

`make e2e` requires `opm` in `PATH` — `TestPlccCheckCatalogPresence` exercises `plcc-check --catalog-image` by pointing `opm render` at a local FBC directory fixture (`testdata/catalog-fbc/`), which needs no registry or network access.

`test/e2e/catalog_test.go` also tests the Go `catalog.Render` API with real `opm`
and local catalog fixtures. It verifies bundle identities and original versions,
lifecycle presence and versions, and compatibility with empty lifecycle package
names. These tests make no registry or PLCC requests. Parser and subprocess error
tests live in `pkg/catalog` and run with `make test` without an installed `opm`.

This complements `pkg/fbc/pipeline_test.go` (integration test at the Go API level) and `cmd/plcc2fbc/main_test.go` (unit tests for the `run()` function). The e2e suite is the only layer that verifies exit code semantics and the full binary's file I/O behavior.

---

## Test Matrix

| Test | Mode | Validators | What It Verifies |
|------|------|------------|------------------|
| `TestSingleFileNoValidators` | single-file | none | Full pipeline output matches `reference-fbc.yaml` |
| `TestSingleFileAllValidators` | single-file | all (default) | Full pipeline output matches `reference-fbc-validated.yaml` |
| `TestSplitNoValidators` | `--split` | none | Per-package directories match segments from `reference-fbc.yaml` |
| `TestSplitAllValidators` | `--split` | all (default) | Per-package directories match segments from `reference-fbc-validated.yaml` |
| `TestSingleFilePackageFilter` | single-file | none | `-p` filter produces output matching a single reference segment |
| `TestExitCode1_InvalidInput` | single-file | all | Nonexistent input file → exit code 1, `Error:` on stderr |
| `TestExitCode2_NoFBCOutput` | single-file | none | Untranslatable data → exit code 2, `no FBC data generated` on stderr |
| `TestExitCode3_MissingPackages` | single-file | all | Missing `-p` package → exit code 3, `requested packages not found` on stderr |
| `TestDumpPLCC` | `--dump-plcc` | none | Dumps filtered PLCC JSON directly, skipping FBC translation; output is valid JSON containing requested package |
| `TestAllowMissing` | single-file | none | `--allow-missing` downgrades missing `-p` package from exit 3 to exit 0; found package still in output |
| `TestJSONOutput` | single-file | none | `-o json` produces valid JSON containing the expected package |
| `TestLogFlag` | single-file | all | `-l` redirects validation report to a file; each line is valid JSON |
| `TestPermissive` | single-file | all | `--permissive` produces at least as many packages as strict mode |
| `TestListValidators` | N/A | N/A | `--list-validators` exits 0 and prints `Groups:` and `Labels:` sections |

`test/e2e/plcc_check_test.go` covers the reporting command:

| Test | Mode | What It Verifies |
|------|------|-------------------|
| `TestPlccCheckOperatorsFile` | 4 selected packages | Summary and validation goldens; accepted FBC reference; structured run log |
| `TestPlccCheckCatalogPresence` | Selected packages + local catalog | Lifecycle-only catalog yields `NO BUNDLES`; shipped version absent from PLCC yields `REGRESSED`; catalog package artifact |
| `TestPlccCheckCatalogVersionCoverage` | 5 selected packages + local catalog | Catalog `OK`, `X/Y`, `MISSING`, `NO BUNDLES`; PLCC precedence; typed missing lifecycle findings |
| `TestPlccCheckCatalogEmptyPackageName` / `TestPlccCheckCatalogNullPackageName` | Empty/null lifecycle names | Ignore nameless records without introducing bogus operators |
| `TestPlccCheckWebhook` | `summary`, `table`, `list`, `details`, and combinations | Exactly the requested section headers, catalog source, table legend, action-grouped CSV lists, icons and labels, scope and workflow artifact link |
| `TestPlccCheckWebhookRejectsUnknownSection` | Invalid section | Exit 1 before assessment |
| `TestPlccCheckScopesCommaSeparatedValidationResult` | Repeated selected alias | One operator row/count; validation output targets only the selected alias |
| `TestPlccCheckSurfacesStaleCatalogPackage` | Catalog-only lifecycle package | `PLCC add`, `MISSING`, `NO BUNDLES`, and regression detail |
| `TestPlccCheckAllPackages` | Full snapshot, validators disabled | FBC matches reference byte-for-byte; 142 operators, 61 OK and 81 invalid |
| `TestPLCCCheckCommand` | Compact fixtures and real local opm | Complete report golden; YAML selection and skip groups; preserved evidence/artifacts and excluded counts; JSON/text/Slack consistency; rendered JSON/opm parity; exit 0 with empty FBC artifact for untranslatable input; fatal input exits 1 |
| `TestPLCCCheckInterruptsFetch` | Stalled local HTTPS proxy | Ctrl+C cancels the PLCC fetch promptly, exits 1 with a cancellation diagnostic, logs the failure, and removes a stale Slack payload |

---

## Testdata Files

| File | Size | Description |
|------|------|-------------|
| `plcc.json` | ~1.5 MB | Real PLCC API snapshot (238 products). Refreshed via `make update-e2e-source`. |
| `reference-fbc.yaml` | ~112 KB | Expected output with `--validators none` (61 packages). |
| `reference-fbc-validated.yaml` | ~59 KB | Expected output with all validators (19 packages). Smaller because validators filter out packages with data quality issues. |
| `untranslatable.json` | ~240 B | Hand-crafted fixture with an invalid version name (`not-a-version`). Used by the exit-code-2 test to produce zero valid FBC output. |
| `plcc-check-operators.txt` | Small | One passing, one invalid, one absent, and one duplicated package. |
| `plcc-check/operators-summary.txt` | Small | Complete summary and details for selected operators without a catalog. |
| `plcc-check/operators-validation.jsonl` | Small | Pipeline failures per affected operator and failure; all original reasons. |
| `catalog-fbc/` | Small | Lifecycle-only entry for `aws-efs-csi-driver-operator`; version `1.0` is absent from the PLCC fixture, exercising regression. |
| `catalog-fbc-versions/` | Small | Bundle and lifecycle coverage for five operators; `cli-manager` lacks PLCC version `0.2`, exercising incompleteness alongside partial catalog coverage. |
| `catalog-fbc-empty-package/`, `catalog-fbc-stale/` | Small | Empty lifecycle name and catalog-only package cases. |
| `plcc-check/catalog-summary.txt` | Small | Selected-operator summary and details with the lifecycle-only catalog. |
| `plcc-check/command-summary.txt` | Small | Complete report for the compact PLCC/catalog fixtures in `internal/plcccheck/testdata`. |

---

## Golden File Update Workflow

When a code change intentionally alters the FBC output (new filter, converter change, schema update), the reference files must be regenerated. Three Makefile targets handle this:

**`make update-e2e`** — Regenerates both reference YAMLs from the existing `testdata/plcc.json`:

```sh
bin/plcc2fbc -i test/e2e/testdata/plcc.json --validators none -o yaml test/e2e/testdata/reference-fbc.yaml
bin/plcc2fbc -i test/e2e/testdata/plcc.json -o yaml test/e2e/testdata/reference-fbc-validated.yaml
```

Use this when your code change altered the output format or filtering behavior. The PLCC input data stays the same.

**`make update-e2e-source`** — Fetches a fresh `plcc.json` from the live PLCC API, then runs `make update-e2e`:

```sh
curl -sSf -o test/e2e/testdata/plcc.json $PLCC_API_URL
make update-e2e
```

Use this to refresh the upstream data snapshot. Both the input and references are updated together.

**`make update-e2e-plcc-check`** — Builds `plcc-check` and regenerates the
selected-operator summary, validation JSONL, catalog summary, and compact command
summary from existing local fixtures. It requires `opm` in `PATH`; no network or
registry access is used. Temporary outputs are cleaned up automatically. Summaries
contain no output-directory paths, so comparison needs no path normalization.

Review all fixture diffs before committing. The reporter preserves more validation
reasons than the former shell script and includes typed missing-content findings
in text and assessment JSON. The validation JSONL contains pipeline failures only.

If full-snapshot counts change, review and update the assertions in
`TestPlccCheckAllPackages`; its FBC output must still match `reference-fbc.yaml`.

---

## Helper Functions

| Function | Purpose |
|----------|---------|
| `runBinary(t, args...)` | Executes the compiled binary, returns stdout, stderr, and exit code. Uses `t.Helper()` for correct error attribution. |
| `extractPackageName(yamlDoc)` | Parses the `package:` field from a YAML document string. |
| `splitYAMLReference(t, path)` | Splits a multi-document YAML file on `---\n` delimiters into a `map[string]string` keyed by package name. |
| `testSplit(t, referencePath, extraArgs...)` | Shared logic for split-mode tests: parses the reference, runs the binary with `--split`, and compares each per-package output file. |
| `runPlccCheck(t, args...)` | Executes the compiled reporting binary with stable GitHub environment variables; captures stdout, stderr, and exit code. |
| `readAssessment(t, dir)` / `assessedPackage(t, report, name)` | Read structured report evidence and select a package for assertions. |
| `slogField(t, line, field)` | Parses one `slog.json` line and returns a named field, failing the test if the line isn't valid JSON or the field is absent. Used to check specific counts without requiring an exact byte-for-byte match (timestamps vary on every run). |
| `assertFilesEqual(t, gotPath, wantPath)` | Compares a generated artifact byte-for-byte with its golden file. |

---

## Adding a New E2E Test

`TestPLCCCheckCommand` uses the shared reporting binary and runner. It compares
real local `opm` rendering with pre-rendered JSON input and checks command-level
exit semantics. Its summary golden is included in `make update-e2e-plcc-check`.

1. Write a test function in `test/e2e/e2e_test.go` (for the `plcc2fbc` binary) or `test/e2e/plcc_check_test.go` (for `plcc-check`). Use `runBinary` or `runPlccCheck` to invoke it with the desired flags.
2. For golden-file comparison: compare output against existing reference files or segments extracted via `splitYAMLReference`.
3. For error-path tests: assert both the exit code and a stderr substring.
4. For split-mode tests: use the `testSplit` helper or follow its pattern.
5. If your test needs a new fixture, add it to `test/e2e/testdata/`. Minimal hand-crafted fixtures (like `untranslatable.json`) are preferred for error-path tests.
6. If comparing a file that embeds non-deterministic data (a temp-dir path, a timestamp, a version string), normalize it before comparing rather than skipping the check — see the `slogField` usage in `TestPlccCheckOperatorsFile`.
7. Run `make e2e` to verify.
