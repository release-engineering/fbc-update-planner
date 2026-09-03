# End-to-End Tests

End-to-end tests exercise the full `plcc2fbc` CLI pipeline — flag parsing, PLCC loading, validation, FBC translation, filtering, and file I/O — by building the binary and invoking it as a subprocess against golden reference files. `test/e2e/plcc_check_test.go` additionally covers `scripts/plcc-check.sh`, the batch runner built on top of the binary.

---

## Architecture

`TestMain` compiles `plcc2fbc` from source into a temporary directory once per test run. All test functions invoke the compiled binary via the `runBinary` helper, which captures stdout, stderr, and the exit code. Output is compared byte-for-byte against reference files in `test/e2e/testdata/`.

`test/e2e/plcc_check_test.go` instead invokes `scripts/plcc-check.sh` directly via `runPlccCheck` — the script builds its own copy of the binary (via `make build`) and requires `scripts/plcc-check.sh`'s `-i <file>` flag to point it at a fixture instead of the live PLCC API.

The e2e package uses a `//go:build e2e` build tag so that `go test ./...` (i.e. `make test`) does not include it. Run with `make e2e` (which passes `-tags=e2e`) to execute the suite.

`make e2e` requires `opm` in `PATH` — `TestPlccCheckCatalogPresence` exercises `scripts/plcc-check.sh --catalog-image` by pointing `opm render` at a local FBC directory fixture (`testdata/catalog-fbc/`), which needs no registry or network access.

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

`test/e2e/plcc_check_test.go` covers `scripts/plcc-check.sh` separately:

| Test | Mode | What It Verifies |
|------|------|-------------------|
| `TestPlccCheckOperatorsFile` | `plcc-check-operators.txt` (4 packages: pass/issues/missing/duplicate) | `summary.txt`, `validation.jsonl`, `fbc-output.yaml`, and `slog.json` message sequence match golden fixtures |
| `TestPlccCheckCatalogPresence` | `plcc-check-operators.txt` + `--catalog-image testdata/catalog-fbc` | `summary.txt` (PLCC/CATALOG table, with a leading "fully done" marker) matches golden; `catalog-packages.txt` lists the one package present in the fixture |
| `TestPlccCheckAllPackages` | no operators file (full dataset), `--validators none` | `fbc-output.yaml` matches `reference-fbc.yaml` byte-for-byte; `summary.txt` reports the expected pass/fail counts |

---

## Testdata Files

| File | Size | Description |
|------|------|-------------|
| `plcc.json` | ~1.5 MB | Real PLCC API snapshot (238 products). Refreshed via `make update-e2e-source`. |
| `reference-fbc.yaml` | ~112 KB | Expected output with `--validators none` (61 packages). |
| `reference-fbc-validated.yaml` | ~59 KB | Expected output with all validators (19 packages). Smaller because validators filter out packages with data quality issues. |
| `untranslatable.json` | ~240 B | Hand-crafted fixture with an invalid version name (`not-a-version`). Used by the exit-code-2 test to produce zero valid FBC output. |
| `plcc-check-operators.txt` | ~150 B | Operators file for `TestPlccCheckOperatorsFile`: one passing package, one with validation issues, one that doesn't exist in `plcc.json`. |
| `plcc-check/operators-summary.txt` | ~1 KB | Expected `summary.txt` for `TestPlccCheckOperatorsFile`. The output directory's absolute path is normalized to `$OUTDIR` before comparison, since it's a fresh `t.TempDir()` on every run. |
| `plcc-check/operators-validation.jsonl` | ~400 B | Expected `validation.jsonl` for `TestPlccCheckOperatorsFile`. |
| `catalog-fbc/` | ~150 B | Local FBC directory fixture for `TestPlccCheckCatalogPresence`: contains lifecycle data for `aws-efs-csi-driver-operator` only (not `barbican-operator`), so `opm render` against it exercises both a catalog-hit and a catalog-miss. |
| `plcc-check/catalog-summary.txt` | ~1 KB | Expected `summary.txt` for `TestPlccCheckCatalogPresence`. Both `$OUTDIR` and the `--catalog-image` path are normalized before comparison. |

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

**`make update-e2e-plcc-check`** — Regenerates `plcc-check/operators-summary.txt`, `plcc-check/operators-validation.jsonl`, and `plcc-check/catalog-summary.txt`, the small, hand-reviewed fixtures for `TestPlccCheckOperatorsFile` and `TestPlccCheckCatalogPresence`:

```sh
out=$(mktemp -d)
./scripts/plcc-check.sh -i test/e2e/testdata/plcc.json -o "$out" test/e2e/testdata/plcc-check-operators.txt
sed "s#$out#\$OUTDIR#g" "$out/summary.txt" > test/e2e/testdata/plcc-check/operators-summary.txt
cp "$out/validation.jsonl" test/e2e/testdata/plcc-check/operators-validation.jsonl

out=$(mktemp -d)
./scripts/plcc-check.sh -i test/e2e/testdata/plcc.json -o "$out" \
    --catalog-image test/e2e/testdata/catalog-fbc test/e2e/testdata/plcc-check-operators.txt
sed -e "s#$out#\$OUTDIR#g" -e "s#test/e2e/testdata/catalog-fbc#\$CATALOG_IMAGE#g" \
    "$out/summary.txt" > test/e2e/testdata/plcc-check/catalog-summary.txt
```

Review the diff carefully — these are hand-reviewed fixtures, not a bulk snapshot. Run this if `TestPlccCheckOperatorsFile` or `TestPlccCheckCatalogPresence` legitimately change behavior (e.g. a change to `scripts/plcc-check.sh` or the validators it exercises).

If `TestPlccCheckAllPackages`'s expected counts (`Total operators`, `PLCC OK`, `PLCC INVALID`, `PLCC MISSING`) change, update the literal strings in `test/e2e/plcc_check_test.go` directly — there's no golden file for that test's `summary.txt`, since diffing the full ~150-package file isn't worth the review overhead.

---

## Helper Functions

| Function | Purpose |
|----------|---------|
| `runBinary(t, args...)` | Executes the compiled binary, returns stdout, stderr, and exit code. Uses `t.Helper()` for correct error attribution. |
| `extractPackageName(yamlDoc)` | Parses the `package:` field from a YAML document string. |
| `splitYAMLReference(t, path)` | Splits a multi-document YAML file on `---\n` delimiters into a `map[string]string` keyed by package name. |
| `testSplit(t, referencePath, extraArgs...)` | Shared logic for split-mode tests: parses the reference, runs the binary with `--split`, and compares each per-package output file. |
| `runPlccCheck(t, args...)` | Executes `scripts/plcc-check.sh` (in `plcc_check_test.go`), returns stdout, stderr, and exit code. Longer default timeout than `runBinary` since the script rebuilds the binary itself. |
| `slogField(t, line, field)` | Parses one `slog.json` line and returns a named field, failing the test if the line isn't valid JSON or the field is absent. Used to check specific counts without requiring an exact byte-for-byte match (the `time` and `version` fields vary on every run). |

---

## Adding a New E2E Test

1. Write a test function in `test/e2e/e2e_test.go` (for the `plcc2fbc` binary) or `test/e2e/plcc_check_test.go` (for `scripts/plcc-check.sh`). Use `runBinary` or `runPlccCheck` to invoke it with the desired flags.
2. For golden-file comparison: compare output against existing reference files or segments extracted via `splitYAMLReference`.
3. For error-path tests: assert both the exit code and a stderr substring.
4. For split-mode tests: use the `testSplit` helper or follow its pattern.
5. If your test needs a new fixture, add it to `test/e2e/testdata/`. Minimal hand-crafted fixtures (like `untranslatable.json`) are preferred for error-path tests.
6. If comparing a file that embeds non-deterministic data (a temp-dir path, a timestamp, a version string), normalize it before comparing rather than skipping the check — see `TestPlccCheckOperatorsFile`'s `$OUTDIR` substitution and `slogField` usage.
7. Run `make e2e` to verify.
