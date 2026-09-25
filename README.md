# fbc-update-planner

`plcc2fbc` fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API, validates and filters PLCC data, and converts it into File-Based Catalog (FBC) blobs.

## Build

```shell
make build
make plcc-check
```

## Run

```shell
bin/plcc2fbc [flags] <output-path>
scripts/plcc-check.sh [-i <plcc.json>] [--catalog-image <image>] [-o <dir>] [operators-file]
```

The flags below apply to `plcc2fbc`, which handles PLCC-to-FBC conversion only.

| Flag | Description |
|------|-------------|
| `-o, --output <format>` | Output format: `json`, `json-pretty`, or `yaml` (default: `json`) |
| `-p, --package <names>` | Comma-separated package names to include (default: all) |
| `-l, --log <file>` | Write validation/filtering report to `<file>` (default: stderr) |
| `-i, --input <file>` | Read PLCC JSON input from `<file>` instead of fetching from API |
| `--dump-plcc` | Dump filtered PLCC JSON instead of generating FBC |
| `--permissive` | Keep packages that fail PLCC validation instead of filtering them out |
| `--allow-missing` | Warn about missing `-p` packages instead of aborting (exit 3) |
| `--validators <list>` | Comma-separated validators to run: labels (e.g. `REQ-DATE-03`) or groups (`all`, `syntax`, `semantic`, `catalog`). Default: `all` |
| `--list-validators` | List available validators and exit |
| `--split` | Write each package to `<dir>/<package>/lifecycle.{json,yaml}`; positional arg is a directory |

`plcc-check.sh` builds and runs the separate Go checker. It loads PLCC data once, reads the JSON stream from `opm render` when `--catalog-image` is set, and writes `summary.txt`, `validation.jsonl`, `slog.json`, FBC output or a PLCC dump, and optional `classification.json` and `slack-payload.json`. Use `scripts/plcc-check.sh --help` for its options. The checker is built for repository workflows; released binaries and the container continue to contain `plcc2fbc` only.

## `plcc2fbc` exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Fatal error (invalid flags, I/O failure, etc.) |
| 2 | No FBC data generated (all packages filtered out) |
| 3 | Requested packages (`-p`) not found in PLCC data (without `--allow-missing`) |

## Generate lifecycle FBC fragments

```shell
make generate-fbc
```

Builds the tool, runs it against the live PLCC API and writes output to the fbc-samples dir

## Testing

```shell
make test    # unit + integration tests
make e2e     # end-to-end tests (builds binary, runs against golden fixtures)
```

See [End-to-End Tests](docs/E2E_TESTS.md) for the e2e test matrix and golden file update workflow.

## Documentation

- [Validation Rules](docs/VALIDATION_RULES.md)
- [FBC Lifecycle Schema](docs/FBC_SCHEMA.md)
- [End-to-End Tests](docs/E2E_TESTS.md)
