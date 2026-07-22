# End-to-End Tests

End-to-end tests exercise the full `plcc2fbc` CLI pipeline — flag parsing, PLCC loading, validation, FBC translation, filtering, and file I/O — by building the binary and invoking it as a subprocess against golden reference files.

---

## Architecture

`TestMain` compiles `plcc2fbc` from source into a temporary directory once per test run. All test functions invoke the compiled binary via the `runBinary` helper, which captures stdout, stderr, and the exit code. Output is compared byte-for-byte against reference files in `test/e2e/testdata/`.

The e2e package uses a `//go:build e2e` build tag so that `go test ./...` (i.e. `make test`) does not include it. Run with `make e2e` (which passes `-tags=e2e`) to execute the suite.

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

---

## Testdata Files

| File | Size | Description |
|------|------|-------------|
| `plcc.json` | ~1.5 MB | Real PLCC API snapshot (238 products). Refreshed via `make update-e2e-source`. |
| `reference-fbc.yaml` | ~112 KB | Expected output with `--validators none` (61 packages). |
| `reference-fbc-validated.yaml` | ~59 KB | Expected output with all validators (19 packages). Smaller because validators filter out packages with data quality issues. |
| `untranslatable.json` | ~240 B | Hand-crafted fixture with an invalid version name (`not-a-version`). Used by the exit-code-2 test to produce zero valid FBC output. |

---

## Golden File Update Workflow

When a code change intentionally alters the FBC output (new filter, converter change, schema update), the reference files must be regenerated. Two Makefile targets handle this:

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

---

## Helper Functions

| Function | Purpose |
|----------|---------|
| `runBinary(t, args...)` | Executes the compiled binary, returns stdout, stderr, and exit code. Uses `t.Helper()` for correct error attribution. |
| `extractPackageName(yamlDoc)` | Parses the `package:` field from a YAML document string. |
| `splitYAMLReference(t, path)` | Splits a multi-document YAML file on `---\n` delimiters into a `map[string]string` keyed by package name. |
| `testSplit(t, referencePath, extraArgs...)` | Shared logic for split-mode tests: parses the reference, runs the binary with `--split`, and compares each per-package output file. |

---

## Adding a New E2E Test

1. Write a test function in `test/e2e/e2e_test.go`. Use `runBinary` to invoke the binary with the desired flags.
2. For golden-file comparison: compare output against existing reference files or segments extracted via `splitYAMLReference`.
3. For error-path tests: assert both the exit code and a stderr substring.
4. For split-mode tests: use the `testSplit` helper or follow its pattern.
5. If your test needs a new fixture, add it to `test/e2e/testdata/`. Minimal hand-crafted fixtures (like `untranslatable.json`) are preferred for error-path tests.
6. Run `make e2e` to verify.
