# fbc-update-planner

`plcc2fbc` fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API, validates and filters PLCC data, and converts it into File-Based Catalog (FBC) blobs.

## Build

```shell
make build
```

## Run

```shell
bin/plcc2fbc [flags] <output-path>
```

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

## Exit Codes

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

## Report PLCC and catalog coverage

```shell
make plcc-check
bin/plcc-check --validators syntax,catalog \
  --catalog-image registry.redhat.io/redhat/redhat-operator-index:v5.0 \
  -o report scripts/top-operators
```

Omit the operators file to assess all packages found in PLCC or the catalog.
Catalog image inspection requires `opm` and registry access. For offline inputs,
Slack payload generation, actions, and artifacts, see [PLCC reporting](docs/PLCC_CHECK.md).
The reporting command exits 0 for findings and 1 for execution errors.

## Testing

```shell
make test    # unit + integration tests
make e2e     # end-to-end tests (builds binary, runs against golden fixtures)
```

See [End-to-End Tests](docs/E2E_TESTS.md) for the e2e test matrix and golden file update workflow.

## Documentation

- [PLCC and Catalog Reporting](docs/PLCC_CHECK.md)
- [Validation Rules](docs/VALIDATION_RULES.md)
- [FBC Lifecycle Schema](docs/FBC_SCHEMA.md)
- [End-to-End Tests](docs/E2E_TESTS.md)
