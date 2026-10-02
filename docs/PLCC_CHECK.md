# PLCC and catalog assessment

`internal/plcccheck` owns the reporting policy for task A in issue #93. It joins
the source-preserving PLCC Dataset API with the catalog inventory and existing
FBC translation pipeline. `cmd/plcc-check` loads inputs and writes artifacts;
text, JSON, and Slack reports share the same assessment. The daily workflows
build and invoke this command directly. It replaces `scripts/plcc-check.sh`;
`plcc2fbc` remains the conversion CLI.

## Reporting command

```sh
make plcc-check
bin/plcc-check --validators syntax,catalog \
  --catalog-image registry.redhat.io/redhat/redhat-operator-index:v5.0 \
  --config scripts/plcc-check-top.yaml -o report

# Fully offline, with a previously rendered JSON stream:
bin/plcc-check -i internal/plcccheck/testdata/plcc.json \
  --catalog-input internal/plcccheck/testdata/catalog.json \
  --validators syntax,catalog -o report
```

Usage: `plcc-check [flags] [operators-file]`. Without a file or config selection,
assess the union of all named PLCC products and catalog packages. Operator files accept blank
lines, comments (including inline `#`), surrounding whitespace, and duplicates;
first-seen order is preserved. An empty file is an error.

| Flag | Purpose |
| --- | --- |
| `-o, --output DIR` | Artifact directory, created if needed; default `.` |
| `-i, --input FILE` | Local PLCC JSON instead of one API fetch |
| `--config FILE` | YAML operator selection and skip groups; mutually exclusive with the positional operators file |
| `--validators CSV` | PLCC rule labels/groups; default `all` |
| `--catalog-image REF` | Image or local directory rendered by `opm` |
| `--catalog-input FILE` | Rendered catalog JSON; mutually exclusive with `--catalog-image` |
| `--plcc` | Save filtered PLCC instead of FBC output |
| `--webhook CSV` | Generate Slack sections: `summary`, `table`, `list`, `details` |
| `-h, --help` | Print usage |

### Operator configuration

Use `--config FILE` for one YAML document containing selection and grouped
reporting exceptions:

```yaml
# Omit selected to assess all PLCC/catalog operators.
selected:
  - cluster-logging
  - openshift-gitops-operator

skipped:
  - reason: "Explanation shared by this group"
    operators:
      - example-operator
      - another-example-operator
```

Each skip group requires a nonempty reason and operator list. Use a group with
one operator for an individual note. Names match exact package names; patterns
are not supported. Skip entries apply only to the assessed selection and never
add rows. Entries outside the selection or absent from both sources are ignored.

Omitting `selected` means all operators; an explicit empty or null selection is
an error. Selection preserves first-seen order and removes duplicates. Names and
reasons have surrounding whitespace removed. Unknown fields, duplicate YAML
keys, blank names/reasons, and repeated operators within or across skip groups
are errors. Configuration is validated before loading PLCC or rendering a catalog.
An empty mapping (`{}`) is valid and selects all operators without skips.

The existing positional operator-list file remains supported, but cannot be
combined with `--config`. Without either input, all operators are assessed.

Skipped operators still undergo all PLCC, FBC translation, and catalog checks.
Their action becomes `SKIPPED` and JSON includes `skipReason`, while every status,
failure, issue, and coverage value is retained. Skipping does not suppress fatal
input/assessment errors or change validation logs and generated PLCC/FBC artifacts.
Human-readable Details show only the skip note for these operators.

JSON summary fields `total`, `nonSkipped`, and `skipped` describe the assessed
set. PLCC/catalog status counts and `fullyOK` exclude skipped operators. All four
CSV groups use the total assessed count as their denominator and partition the
full selection. The summary explicitly labels status counts as excluding skips.

Findings, missing requested operators, and zero translatable products **exit 0**:
they are report results. Argument, input, catalog parsing/rendering, assessment,
and output failures **exit 1**. This command does not implement task B's build
gate or permissive policy.

Ctrl+C cancels in-flight PLCC HTTP requests, response reads, and retry delays,
as well as catalog rendering. Cancellation exits 1 and removes the Slack payload
so a failed run cannot expose a stale success notification.

`--plcc` controls the saved artifact only. Assessment always includes mandatory
FBC conversion and filtering, so untranslatable PLCC cannot receive `OK`. This
differs from the old script's validation-only `--plcc` mode. Its PLCC dump still
contains products that passed the selected PLCC validators, preserving aliases
and product shape even when subsequent FBC conversion rejects them.

### Artifacts

| File | Contents |
| --- | --- |
| `summary.txt` | Catalog source, complete summary/table/CSV lists/details; byte-for-byte identical to stdout |
| `assessment.json` | Catalog source, summary counts and structured assessment with all evidence and original reasons |
| `validation.jsonl` | Pipeline failures in `report.ValidationResult` format, per affected operator and failure; missing-content issues are in the full reports |
| `slog.json` | JSON run log, including fatal errors after output initialization |
| `fbc-output.yaml` | Successfully translated whole products, sorted by package |
| `plcc-dump.json` | Filtered selected PLCC, replacing FBC output with `--plcc` |
| `catalog-packages.txt` | All catalog packages with lifecycle entries, sorted; only when a catalog is checked |
| `slack-payload.json` | Slack webhook JSON; only with `--webhook` |

The command reserves these filenames in the output directory. It stages report
artifacts before publishing and publishes Slack last. Each file replacement is
atomic, but replacing the entire set is not a filesystem transaction. Once
arguments are accepted and output initialization starts, a previous Slack
payload is removed so a failed rerun cannot expose a stale success notification.
Successful reruns also remove optional artifacts that no longer apply. Other
previous artifacts may remain after a failed run; use a fresh output directory
per workflow run and check the exit status.

### Slack

`--webhook summary,table,list,details` generates a payload without sending it.
Sections are independently selectable and appear in this order:

- `summary`: operator counts by status.
- `table`: Action/Operator/PLCC/Catalog/Skipped rows.
- `list`: CSV operator names grouped as OK, PLCCDATA (PLCC add and fix),
  OPERATOR (operator add and build), and SKIPPED. Names retain assessment order
  within each group.
- `details`: findings grouped by package.

Each selected Slack section starts with a larger, bold
[header](https://docs.slack.dev/reference/block-kit/blocks/header-block/).
These headers share the message's block and character budgets.

**Selector change:** the previous `list` section is now named `table`; `list`
now means action-grouped CSV lists. Use `--webhook summary,table` to retain the
previous summary-and-table presentation. The text and JSON artifacts retain the
complete assessment regardless of Slack section selection; no CSV artifact is
added.

Both the text summary and Slack's `summary` section show
`Catalog image: <reference>` when `--catalog-image` is used, preserving the
reference as supplied (including local directory references). With
`--catalog-input`, the label is `Catalog input: <file>`. No source line is
shown when no catalog was supplied. The JSON report records these optional
values as `catalogImage` or `catalogInput`.

The source is followed by one blank line, then the results in this order:

```text
Catalog image: <reference>

Total operators: 168
Skipped operators: 46    (Status counts exclude skipped operators)
READY operators: 9/122
PLCC: ...
Catalog: ...
```

`READY operators` counts operators with both statuses OK over the number of
non-skipped operators. It immediately follows the skipped count and is a larger,
bold Slack header. There is no separate non-skipped count line. A checked report
with no non-skipped operators displays `0/0`; without a catalog check, READY is
omitted and the existing unchecked-catalog explanation is retained. The JSON
fields `fullyOK` and `nonSkipped` and their counting semantics are unchanged.

Each list heading includes its action icon, an explanation, and the group count
over the total number of operators in the report. For example:

```text
✅ OK - operators ready 2/8
📋 PLCCDATA - operators that need PLCC fixes 4/8
📦 OPERATOR - operators that need a catalog rebuild 2/8 (PLCC data ready, missing bundles or lifecycle data in the catalog)
➖ SKIPPED - operators excluded from action reporting 0/8
```

Counts describe all assessed operators, including names omitted from Slack due
to message limits. The denominator follows operator selection, not the size of
the entire catalog. Names appear in a separate copyable CSV block, with commas
and quotes escaped as CSV fields. Empty groups show `0/total` and `None`; an
empty report uses `0/0`. Large groups split between names into independently
valid CSV records with repeated action headings marked `(continued)`, retaining
the full group count. Without a catalog check, the OK heading reads
`OK - operators with PLCC data ready X/Y (catalog not checked)`.

`summary.txt` also contains a `List` section between the table and details, with
the same four CSV groups, icons, and ordering as Slack. Its
lists are complete even when the Slack payload must omit names. This section is
always written, including when `--webhook` is not requested.

The shared text and Slack legend reads: "PLCC/Catalog X/Y: X versions available,
Y versions required." It appears beside the table, rather than among summary counts.
Both columns use the same **Y**: the number of distinct bundle MAJOR.MINOR
versions required by the catalog. **X** counts those required versions present
in source PLCC for the PLCC column, or shipped lifecycle data for CATALOG.
Extra versions in either source do not increase X. The text report uses the same
placement and explanation.

Payload generation requires `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY`, and
`GITHUB_RUN_ID`, used to link to the workflow run and its artifacts. Operator rows
use padded columns in Slack's
[preformatted rich text blocks](https://docs.slack.dev/reference/block-kit/block-elements/rich-text-preformatted-element/),
with action icons and a repeated column header in each chunk. The ACTION column
shows 📋 PLCCDATA for either PLCC action, 📦 OPERATOR for either operator action,
✅ alone for OK, and ➖ alone for skipped operators. The text table uses the same
ACTION labels and icons. The SKIPPED column contains the explanation and is empty for ordinary
rows. Grouped CSV headings are ✅ OK, 📋 PLCCDATA, 📦 OPERATOR, and ➖ SKIPPED.
The PLCC and CATALOG columns display `OK` as ✅; text and JSON retain `OK`.
Pipe-separated text in a `plain_text` section does not render as a table.
Names remain literal text
inside the preformatted block; reasons use plain text sections, so neither can
become mentions or formatting. Control characters in names are escaped before
CSV encoding, as in table rows. Long reports explicitly count omitted report
lines and omitted operator names from action lists, and link to the full
artifacts. No individual name or finding is silently shortened. The renderer respects
Slack's [50-block message limit](https://docs.slack.dev/reference/block-kit/blocks/)
and [3000-character section limit](https://docs.slack.dev/reference/block-kit/blocks/section-block/),
with the same 3000-character cap for preformatted blocks and an additional
35,000-character budget shared by all sections. Large tables and lists can leave details
available only in the linked artifacts.

## Daily workflows

The all-operator and top-operator workflows build the reporter with
`make plcc-check`, then run `bin/plcc-check --webhook summary,table,list
--validators syntax,catalog --catalog-image
registry.redhat.io/redhat/redhat-operator-index:v5.0 -o "$RUN_OUTPUT"`.
Each passes its own `--config`: `scripts/plcc-check-all.yaml` or
`scripts/plcc-check-top.yaml`. The former omits `selected`; the latter contains
the top-operator selection. Maintain skip groups independently in these files.
Both contain skip groups for `non-fbc operator` and `ocp art operator` exceptions.
`scripts/top-operators` remains available to legacy local callers.

Both workflows install `opm`, authenticate to the registry, and use a fresh
artifact directory for each run. The command creates the Slack payload; the
workflow uploads the report artifacts and posts the payload. Execution or build
failures use the workflow's fallback notification and fail the workflow. Report
findings alone do not fail it. The shell reporting script has been removed;
local callers should build and invoke `bin/plcc-check` too.

## Entry point

```go
assessment, err := plcccheck.Assess(source, inventory, plcc.DatasetOptions{
    Packages:   packages,
    Validators: []string{"syntax", "catalog"},
})
```

- `source` is one PLCC snapshot, already fetched or loaded by the caller.
- `inventory` is already parsed or rendered using `pkg/catalog`. Nil skips
  catalog checks; a non-nil empty inventory represents an empty catalog.
- `Packages` follows Dataset semantics: nil selects all packages; a non-nil
  empty slice selects none. Explicit names are deduplicated in request order.
  All-package reports use alphabetical order and include the union of PLCC,
  catalog bundle, and catalog lifecycle packages. Unnamed PLCC context products
  remain available to validators without becoming report rows.
- `Validators` follows Dataset semantics: empty means `all`, and `none` disables
  PLCC checks. Mandatory FBC converters and default filters always apply to
  products that pass the selected PLCC checks.

The function performs no I/O and leaves inputs unchanged. Its result is ordinary,
caller-owned data. Errors in options or assessed catalog version syntax return
nil, never a partial report. Missing PLCC and rejected products are assessment
results, not execution errors.

After assessment, call `assessment.ApplySkips(map[string]string)` with exact
package names mapped to notes, then `NewReport(assessment)`. `ApplySkips` replaces
the reporting policy, changes only actions and skip notes, and ignores names
outside the assessed set. Passing nil clears exceptions and restores normal
actions. Blank names or notes are errors and leave the assessment unchanged.
The CLI expands YAML skip groups into this mapping; the Dataset API is unchanged.

`Assessment.CatalogChecked` distinguishes unchecked and empty catalogs even when
there are no selected packages. `FilteredPLCC` and `FBC` retain pipeline outputs
from the same validation/translation pass for artifact generation, without another
fetch or conversion. They are independent of the caller's source and omitted from
assessment JSON. `FilteredPLCC` excludes PLCC validation failures; `FBC` also
excludes conversion/filter failures. Disabling duplicate validation can retain
multiple translated products for a package; the assessment does not collapse them.

## Evidence and identity

Each package assessment includes source product indices, unique raw source
version names, successfully produced lifecycle versions, structured failures,
optional catalog coverage, typed issues, and one operator-level action.

Source indices refer to the supplied PLCC snapshot. Every matching product
contributes evidence; duplicate package names never discard later products.
Comma-separated aliases follow Dataset selection and validation semantics. A
failure can target one alias while rejecting the entire source product. The
failure retains its actual target, and every affected alias retains the failure.
Only the target of `REQ-VAL-01` receives the `DUPLICATE` PLCC status; another alias
blocked by that product's rejection receives `INVALID`.

PLCC failures retain validator label, group, scope, targets, and original reasons.
FBC failures retain their source index and original reasons, including converter
and filter labels. The assessment does not extract metadata from reason strings.
Translation follows the existing pipeline: rejected PLCC products are skipped,
and converter/filter failures reject the whole translated product. No attempt is
made to translate isolated versions from a rejected product.

## Version comparison and coverage

Bundle versions accept canonical semantic versions and plain `MAJOR.MINOR`
shorthand for compatibility with the former reporting script. Patch, prerelease,
and build components collapse to one distinct `MAJOR.MINOR`. Original bundle
names and versions remain in the coverage evidence. Invalid version strings,
leading zeros in numeric core/prerelease components, and major/minor values
outside the FBC type's range are errors with package/bundle context.

Catalog lifecycle version names must parse as FBC `MAJOR.MINOR`. PLCC source
names are retained verbatim and matched exactly to the required `MAJOR.MINOR`:
a malformed source name such as `1.2.3` does not establish the presence of `1.2`.
The malformed source remains visible in failures and raw version evidence.
Catalog syntax checks apply to the assessed package selection.

Normalized version lists are numerically sorted and deduplicated.
Coverage counts distinct bundle `MAJOR.MINOR` values present in lifecycle data:

- `NO BUNDLES`: zero bundles, regardless of lifecycle entry presence.
- `MISSING`: bundles exist but there is no lifecycle entry.
- `OK`: bundles exist and every required version is present in lifecycle data.
- `X/Y`: an entry exists but only X of Y required versions are present.

These statuses apply in the order listed. An empty lifecycle entry with bundles
produces `0/Y`. Coverage evaluates version presence only; it does not inspect
shipped phases or compatibility data. PLCC versions without bundles do not imply
missing bundles: only zero bundles produces `NO BUNDLES`.

## Completeness and regression findings

`PackageAssessment.Issues` replaces the former action-bearing `Gaps`. Each `Issue`
contains a typed `Kind` and an optional `Version` (`MAJOR.MINOR`). Package-level
issues have no version. Version findings have no action of their own.

| Issue kind | Evidence |
| --- | --- |
| `plcc-package-missing` | Package absent from all source PLCC products |
| `plcc-version-missing` | Bundle requires a version absent from both source PLCC and catalog lifecycle data |
| `plcc-version-regressed` | Shipped lifecycle version absent from source PLCC |
| `catalog-bundles-missing` | Operator has zero bundles |
| `catalog-lifecycle-missing` | No lifecycle entry exists for the operator |
| `catalog-lifecycle-version-missing` | Required bundle MAJOR.MINOR absent from catalog lifecycle data |

PLCC completeness checks **all bundle versions**, including those already covered
by lifecycle data. Regression checks **all shipped lifecycle versions**, including
versions without corresponding bundles and packages with no bundles. A regression
describes the discrepancy between current PLCC and shipped lifecycle data; it
does not establish when or why the data disappeared.

A version absent from PLCC but present in shipped lifecycle data gets a regression
issue instead of an ordinary missing-PLCC-version issue. Whole-package absence
also retains every version finding. Missing lifecycle entries retain both the
entry-level issue and each uncovered bundle version, so no affected version is
lost. The absence of bundles and lifecycle data produces both package-level issues.

Issues are deterministic: package-level issues first (PLCC package, bundles,
lifecycle entry), then version issues in numeric order, PLCC before catalog for
the same version. Each distinct affected MAJOR.MINOR is reported once per kind.
Validation/conversion failures remain in `Failures`, retaining all original
reasons and metadata independently of these issues.

## PLCC status and operator action

Each operator receives exactly one PLCC status, with this precedence:

| Status | Condition |
| --- | --- |
| `MISSING` | Package absent from source PLCC |
| `DUPLICATE` | Selected duplicate-package validation rejected this package |
| `INVALID` | PLCC validation or mandatory FBC conversion/filtering rejected a product |
| `REGRESSED` | Any shipped lifecycle version is absent from current PLCC |
| `INCOMPLETE` | Existing package lacks any required bundle MAJOR.MINOR version |
| `OK` | None of the above |

An existing product can be `INCOMPLETE` even if all required versions are absent.
Text and Slack tables display this status as X/Y (including 0/Y), using the
required bundle versions present in source PLCC. The JSON classification and
summary counts retain `INCOMPLETE`; other statuses keep their labels and precedence.
`MISSING` is reserved for an absent package. Validation failures take precedence
over regressions and incompleteness; all underlying findings remain in JSON and,
for non-skipped operators, human-readable details.

`Action` is used only for the operator-level recommendation:

1. **PLCC add** when the package is absent from PLCC.
2. **PLCC fix** for `DUPLICATE`, `INVALID`, `REGRESSED`, or `INCOMPLETE` PLCC.
3. **OPERATOR add** when PLCC is `OK` and the checked catalog has no bundles,
   even if it contains lifecycle data for the package.
4. **OPERATOR build** when PLCC is `OK`, bundles exist, and catalog lifecycle
   data is missing or incomplete.
5. **OK** when all applicable checks pass.

An explicit reporting exception overrides this recommendation with **SKIPPED**,
without changing PLCC or catalog status.

Any product rejection blocks a build recommendation, even if another product
for that package translated successfully with duplicate validation disabled.
Produced version evidence and all catalog issues are retained for investigation.

Without a catalog, no completeness or regression checks are possible. Catalog
evidence is nil, and issues can only report an absent PLCC package. Validation
and mandatory translation still run. Successful assessment receives action `OK`,
which renderers must qualify as **PLCC OK; catalog not checked**. It does not
count as fully ready in both PLCC and catalog.

## Report contract

The reporting command and renderers consume this model in two parts:

1. **Summary:** catalog source followed by a blank line, total checked operators,
   skipped operators with the inline exclusion note, READY operators, then counts by PLCC status (`OK`, `MISSING`,
   `INCOMPLETE`, `INVALID`, `REGRESSED`, `DUPLICATE`), counts by catalog status
   (`OK`, `MISSING`, `INCOMPLETE`, `NO BUNDLES`). Follow with
   one row per operator: **Action | Operator | PLCC | Catalog | Skipped**. Counts describe
   operators, not findings; only non-skipped rows contribute to each applicable status
   breakdown. PLCC and catalog `X/Y` rows count as `INCOMPLETE`.
   READY excludes skipped operators and requires a catalog check and both statuses `OK`;
   its denominator is the non-skipped count.
   The summary identifies the catalog source; the text report follows its table
   with the complete OK, PLCCDATA, OPERATOR, and SKIPPED CSV lists.
2. **Details:** all findings grouped by package. Emit one line per original
   validation/conversion reason, retaining its rule labels, and one line per
   typed issue, including its version when present. When the entire catalog
   lifecycle entry is missing, text and Slack report that once and omit the
   redundant catalog lifecycle version findings. Missing PLCC versions still
   appear, and the JSON assessment retains all version findings. An existing
   lifecycle entry with missing versions, including an empty entry, still gets
   per-version details. Status precedence does not suppress details. Operators
   with no findings need no detail section. Skipped operators always receive
   only `[SKIPPED] <reason>` here; their findings remain in assessment JSON.

Catalog counts are omitted when no catalog was checked, and the catalog table
column is marked as not checked. JSON retains specific action labels, including
`SKIPPED` for exceptions. Both text and Slack tables summarize actions as
📋 PLCCDATA, 📦 OPERATOR, ✅, or ➖.
Grouped CSV headings are ✅ OK, 📋 PLCCDATA (both PLCC actions), 📦 OPERATOR
(both operator actions), and ➖ SKIPPED.

**JSON field rename:** `summary.catalog.partial` is now
`summary.catalog.incomplete`, matching the PLCC summary counter. The old key is
no longer emitted; JSON consumers must use `incomplete`.

`NewReport` computes summary counts once for text, JSON, and Slack rendering.
Renderers do not reclassify findings or rerun validators. Do not mutate the
assessment while rendering a report.
Task B's build gate, permissive policy, and required-version CLI flags are outside
this layer's current scope.

## Tests

`go test ./internal/plcccheck` uses local PLCC and rendered catalog fixtures. It
covers the five actions, status precedence, duplicate identities and aliases,
selected validators, mandatory conversion/filter failures, and whole-product
blocking. Completeness and regression cases include full catalog coverage,
lifecycle-only versions, zero bundles, absent packages, and coexisting validation
failures. Patch grouping, malformed versions, selected/all-package runs,
absent/empty catalogs, input ownership, and deterministic results remain covered.
No live API, registry, or `opm` is needed.

Renderer tests also cover status counts, complete package-grouped details,
unchecked/empty selections, pipeline artifact ownership, literal source text,
and Slack truncation, including literal and oversized skip notes. Skip tests
cover preserved evidence, policy replacement, all-skipped and unchecked-catalog
runs, excluded counts, and four mutually exclusive CSV groups.
`go test ./cmd/plcc-check` exercises strict grouped YAML configuration, flags, offline inputs,
artifact contents, reuse of output directories, mandatory translation with
`--plcc`, fatal errors, and Slack payload generation. The command E2E test builds
the binary, checks a reviewed summary fixture and exit codes, and compares local
`opm` rendering with pre-rendered JSON input; see `docs/E2E_TESTS.md`.
