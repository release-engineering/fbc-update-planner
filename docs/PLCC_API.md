# PLCC dataset API

`pkg/plcc` loads PLCC data, selects products, and validates data quality. A
`Dataset` owns the complete source snapshot, a working catalog, the enabled
validators, and their findings. FBC conversion and decisions about logging or
exit status belong to callers.

## Package layout

- `plcc.go` contains the Dataset API, including configuration, construction,
  accessors, validation, and filtering.
- `catalog.go` contains the source data model, catalog operations, selection and
  copy helpers, timestamps, and package lookup errors.
- `client.go` handles the API endpoint, fetching, retries, and local JSON loading.
- `validation.go` defines the validator registries, rule selection, and checks;
  `findings.go` defines their structured results.
- `legacy.go` contains the compatibility API.

Dataset, catalog, client, and compatibility tests follow the same file layout.

`FetchContext(ctx)` and `FetchFromContext(ctx, url, client)` propagate cancellation
and deadlines through HTTP requests, response reads, and retry delays. `Fetch`
and `FetchFrom` use a background context for compatibility. Fetches still make
up to three attempts with exponential backoff; the default HTTP client has a
30-second timeout per request. Cancellation errors remain identifiable with
`errors.Is`.

## Example

```go
source, err := plcc.Fetch() // or plcc.Load(path)
if err != nil {
    return err
}
data, err := plcc.NewDataset(source, plcc.DatasetOptions{
    Packages:   []string{"operator-a", "operator-b"},
    Validators: []string{"syntax", "catalog"},
})
if err != nil {
    return err
}

// The caller decides whether missing packages should stop this run.
if missing := data.MissingPackages(); len(missing) > 0 {
    return &plcc.PackagesNotFoundError{Names: missing}
}

findings := data.Validate()
original := data.Source()
for _, result := range findings.Products {
    product := original.Data[result.SourceIndex]
    for _, failure := range result.Failures {
        fmt.Printf("%s (%v): %s [%s] targets %v: %v\n",
            product.Name, result.Packages, failure.Validator.Label,
            failure.Validator.Scope, failure.Packages, failure.Reasons)
    }
}

// For permissive processing, omit this call and retain the same findings.
if err := data.FilterInvalid(); err != nil {
    return err
}
catalog := data.Catalog()
catalog.ExpandPackages()
catalog.SortByPackage()
// Pass catalog.Data to the FBC translation API.
```

## Configuration and ownership

`NewDataset` copies the complete source, including nested versions and phases.
Products without package names remain in that snapshot, making OCP lifecycle
data available when initializing validators. Modifying the input after
construction cannot change the dataset or the context bound into its rules.

Configuration is fixed at construction. Use a new dataset to change package or
validator selection. `Packages` contains individual package names; nil selects
all products with a package name, while an explicitly empty slice selects none.
Explicit selection retains only matching names within comma-separated product
package fields. Products are sorted stably by that field and remain unexpanded.
`MissingPackages()` returns absent requested names in request order; validation
rejection never makes a source product missing.

An empty `Validators` list defaults to `all`. Groups (`all`, `none`, `syntax`,
`semantic`, `catalog`) and individual labels have their existing meanings.
Overlapping selections run each rule once. `none` must be used alone.
`Validators()` returns resolved rule metadata in registry order; one rule may
contain multiple callbacks. Unknown selectors, validator initialization errors,
and a nil source cause construction to fail. An empty catalog is valid input.

`Source()`, `Catalog()`, `MissingPackages()`, `Validators()`, and `Validate()`
return independent data. Changes to returned catalogs, metadata, or findings do
not affect subsequent calls. A dataset must be constructed with `NewDataset` and
is not safe for concurrent use.

## Findings and filtering

`Validate()` runs catalog rules against the selected products, then product
rules against every selected product, including products with catalog failures.
It leaves both catalogs intact. The complete report is cached; later calls
return copies of that report, including after filtering.

`ValidationReport.Products` contains a `ProductValidation` for every selected
product, including passing products. Each record has its original `SourceIndex`,
selected `Packages`, and `Failures`. Source indices retain product identity
across sorting and filtering; two products with the same package name remain
separate records.

Each `ValidationFailure` contains its `ValidatorInfo` (`Label`, `Group`, and
`Scope`), targeted `Packages`, and original `Reasons`. Metadata comes from the
registry, so consumers can recognize rules without parsing reason strings.
Scopes are `ProductScope` and `CatalogScope`, independent of selectable groups.
Failures are ordered by catalog rules first, then product rules, preserving
registry order and callback message order. Within a catalog rule, targets follow
the product's package order.

A product failure targets all selected names for that product. A catalog failure
targets the particular rejected package and is attached to every affected
product. For example, if products `alpha,beta` and `beta` duplicate `beta`, both
records receive the catalog failure targeting `beta`. Filtering removes both
products, including the first product's `alpha` output. Their distinct source
versions remain available for inspection.

`FilterInvalid()` requires prior validation and removes every working product
with a failure. It preserves the original source, missing-package list, and
validation report. Repeated filtering has no further effect. Omitting filtering
retains invalid products for permissive processing. Passing PLCC validation does
not guarantee FBC conversion will succeed; mandatory converters and filters
remain the responsibility of `pkg/fbc`.

## Compatibility

The pre-dataset Go API remains available in `pkg/plcc/legacy.go`, sharing
selection, rule resolution, and execution helpers with the new API. The existing
CLI continues using that API during this first migration step. Its behavior and
output are preserved, including these differences from dataset defaults:

- `Catalog.FilterByPackageNames(nil)` selects no products.
- `Catalog.LookupValidators()` with no selectors returns no rules.
- `Catalog.Validate()` with no callbacks uses the default catalog rules.
- Legacy selection methods preserve input order until the caller sorts it.

The existing data types, fetch/load functions, validator callbacks, and catalog
serialization/expansion methods remain available.

New callers should use the dataset API. Formal Go deprecation annotations are
deferred until repository callers migrate, so the compatibility stage continues
to pass staticcheck without suppressing diagnostics in those callers.
