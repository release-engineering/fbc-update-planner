# fbc-update-planner

`plcc2fbc` fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API, validates and filters PLCC data, and converts it into File-Based Catalog (FBC) blobs.

**Download latest generated FBC file:** [fbc-latest.yaml](https://github.com/release-engineering/fbc-update-planner/raw/main/fbc-samples/fbc-latest.yaml)

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
| `-l, --log <file>` | Write operational logs to `<file>` (default: stdout) |
| `-i, --input <file>` | Read PLCC JSON input from `<file>` instead of fetching from API |
| `--dump-plcc` | Dump filtered PLCC JSON instead of generating FBC |
| `--strict` | Treat PLCC validation warnings as errors and filter out failing packages |
| `--validators <list>` | Comma-separated validators to run: labels (e.g. `REQ-DATE-03`) or groups (`all`, `syntax`, `semantic`, `catalog`). Default: `all` |
| `--list-validators` | List available validators and exit |
| `--split` | Write each package to `<dir>/<package>/lifecycle.{json,yaml}`; positional arg is a directory |

## Generate FBC snapshot

```shell
make generate-fbc
```

Writes `fbc-samples/fbc-YYMMDD.yaml` and updates `fbc-samples/fbc-latest.yaml`.

## Documentation

- [Validation Rules](docs/VALIDATION_RULES.md)
- [FBC Lifecycle Schema](docs/FBC_SCHEMA.md)
