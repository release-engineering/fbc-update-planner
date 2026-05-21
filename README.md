# fbc-update-planner

`plcc2fbc` fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API and converts it into File-Based Catalog (FBC) YAML blobs.

**Download latest generated FBC file:** [fbc-latest.yaml](https://github.com/release-engineering/fbc-update-planner/raw/main/fbc-samples/fbc-latest.yaml)

## Build

```shell
make build
```

## Run

```shell
bin/plcc2fbc [flags]
```

| Flag | Description |
|------|-------------|
| `-w <file>` | Write FBC data to `<file>` (required) |
| `-i <file>` | Read PLCC JSON input from `<file>` instead of fetching from API |
| `-dump-plcc <file>` | Write filtered PLCC JSON dump to `<file>` |

## Generate FBC snapshot

```shell
make generate-fbc
```

Writes `fbc-samples/fbc-YYMMDD.yaml` and updates `fbc-samples/fbc-latest.yaml`.

## Documentation

- [Validation Rules](docs/VALIDATION_RULES.md)
- [FBC Lifecycle Schema](docs/FBC_SCHEMA.md)
