#!/usr/bin/env bash
# Copyright 2026.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# Naming convention: SCREAMING_CASE marks write-once, constant-like values
# (paths, files); a "g_" prefix marks mutable state shared across functions;
# everything else is function-local (declared with "local"). A leading "_"
# on a function name marks it as a single-purpose helper for one specific
# caller, not general-purpose (unlike log_info/log_error).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

_usage() {
    cat <<EOF
Usage: $(basename "$0") [options] [operators-file]

Run plcc2fbc against a list of operators and summarize results. If
<operators-file> is omitted, all packages found in the PLCC data are
processed.

Arguments:
  [operators-file]   File with one operator name per line (blank lines and
                     lines starting with # are ignored). If omitted, every
                     package present in the PLCC data is checked.

Options:
  -o <dir>           Output directory for generated files (default: current directory)
  -i <file>          Read PLCC JSON from a file instead of fetching from the API
                     (passed through to plcc2fbc; mainly useful for testing)
  --plcc             Validate PLCC data only (skip FBC generation)
  --validators <v>   Comma-separated validators to run (passed through to plcc2fbc;
                     use "none" to skip PLCC validation entirely)
  --catalog-image <ref>  Also check whether each operator's FBC lifecycle data is
                     present in the given OCP catalog image and that every shipped
                     bundle version (MAJOR.MINOR) has a matching lifecycle entry.
                     Reports OK (all covered or no bundles), X/Y (partial), or MISSING
                     (no lifecycle entry at all). Off by default; requires opm.
  --webhook <sections>  Write a Slack webhook payload to <dir>/slack-payload.json.
                     Supported sections: summary,list (comma-separated).
  -h                 Show this help

Example usage:
./plcc-check.sh -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh -o \$(date +%y%m%d) > summary.txt
./plcc-check.sh --plcc -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators none -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators syntax -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --webhook summary,list -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --catalog-image registry.redhat.io/redhat/redhat-operator-index:v5.0 \\
    -o \$(date +%y%m%d) top-operators > summary.txt
EOF
}

# Globals populated by parse_args() and consumed throughout the script.
g_outdir="."
g_input_file=""
g_validate_only=false
g_plcc_validators=""
g_operators_file=""
g_catalog_image=""
g_webhook_sections=""

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -o)
                if [[ $# -lt 2 ]]; then
                    echo "Error: -o requires a value" >&2
                    _usage >&2
                    exit 1
                fi
                g_outdir="$2"; shift 2 ;;
            -i)
                if [[ $# -lt 2 ]]; then
                    echo "Error: -i requires a value" >&2
                    _usage >&2
                    exit 1
                fi
                g_input_file="$2"; shift 2 ;;
            --plcc) g_validate_only=true; shift ;;
            --validators)
                if [[ $# -lt 2 ]]; then
                    echo "Error: --validators requires a value" >&2
                    _usage >&2
                    exit 1
                fi
                g_plcc_validators="$2"; shift 2 ;;
            --catalog-image)
                if [[ $# -lt 2 ]]; then
                    echo "Error: --catalog-image requires a value" >&2
                    _usage >&2
                    exit 1
                fi
                g_catalog_image="$2"; shift 2 ;;
            --webhook)
                if [[ $# -lt 2 ]]; then
                    echo "Error: --webhook requires a value" >&2
                    _usage >&2
                    exit 1
                fi
                if [[ -z "$2" ]]; then
                    log_error "--webhook requires a comma-separated list of sections: summary,list"
                    exit 1
                fi
                g_webhook_sections="$2"; shift 2 ;;
            -h) _usage; exit 0 ;;
            -*) _usage >&2; exit 1 ;;
            *) break ;;
        esac
    done

    if [[ $# -eq 0 ]]; then
        g_operators_file=""
    elif [[ $# -eq 1 ]]; then
        g_operators_file="$1"
    else
        _usage >&2
        exit 1
    fi

    if [[ -n "$g_webhook_sections" ]]; then
        local section
        local -a sections
        if [[ "$g_webhook_sections" == ,* || "$g_webhook_sections" == *, || "$g_webhook_sections" == *,,* ]]; then
            log_error "--webhook requires a comma-separated list of sections: summary,list"
            exit 1
        fi
        IFS=, read -ra sections <<< "$g_webhook_sections"
        for section in "${sections[@]}"; do
            if [[ "$section" != "summary" && "$section" != "list" ]]; then
                log_error "unsupported webhook section: $section (supported: summary,list)"
                exit 1
            fi
        done
    fi
}

log_info() {
    printf '%s\n' "$*" | tee -a "$FILE_SUM"
}

log_error() {
    printf 'Error: %s\n' "$*" >&2
}

check_dependencies() {
    if ! command -v jq &>/dev/null; then
        log_error "jq is required but not found in PATH"
        exit 1
    fi
    if ! command -v tee &>/dev/null; then
        log_error "tee is required but not found in PATH"
        exit 1
    fi
    if [[ -n "$g_catalog_image" ]] && ! command -v opm &>/dev/null; then
        log_error "opm is required for --catalog-image but not found in PATH"
        exit 1
    fi
}

build_plcc2fbc() {
    log_info "Building plcc2fbc..."
    make -C "$ROOT_DIR" build --quiet
}

# Reads g_operators_file into g_operators, skipping blank/comment lines.
_read_operators_file() {
    if [[ ! -f "$g_operators_file" ]]; then
        log_error "file not found: $g_operators_file"
        exit 1
    fi
    g_operators=()
    while IFS= read -r line; do
        line="${line%%#*}"
        line="${line#"${line%%[![:space:]]*}"}"
        line="${line%"${line##*[![:space:]]}"}"
        [[ -z "$line" ]] && continue
        g_operators+=("$line")
    done < "$g_operators_file"

    if [[ ${#g_operators[@]} -eq 0 ]]; then
        log_error "no operator names found in $g_operators_file"
        exit 1
    fi
    # Package selection is set-based. Deduplicate repeated file entries while
    # preserving their first-seen order so totals and result buckets use the
    # same cardinality.
    printf '%s\n' "${g_operators[@]}" | awk '!seen[$0]++' > "$FILE_REQUESTED"
    g_operators=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && g_operators+=("$line")
    done < "$FILE_REQUESTED"
    g_operators_number=${#g_operators[@]}

    local pkg_list
    pkg_list="$(IFS=,; echo "${g_operators[*]}")"
    g_plcc2fbc_args+=(--allow-missing -p "$pkg_list")
}

# Builds g_plcc2fbc_args, runs the binary, and aborts on fatal errors.
run_plcc2fbc() {
    g_plcc2fbc_args=(-o yaml -l "$FILE_VAL")
    if [[ -n "$g_input_file" ]]; then
        g_plcc2fbc_args+=(-i "$g_input_file")
    fi

    g_operators_number="all"
    if [[ -n "$g_operators_file" ]]; then
        _read_operators_file
    fi

    if [[ -n "$g_plcc_validators" ]]; then
        g_plcc2fbc_args+=(--validators "$g_plcc_validators")
    fi
    if $g_validate_only; then
        g_plcc2fbc_args+=(--dump-plcc)
        log_info "Running plcc2fbc with ${g_operators_number} operators (PLCC validation only)..."
    else
        log_info "Running plcc2fbc with ${g_operators_number} operators..."
    fi

    local exit_code
    set +e
    "$ROOT_DIR/bin/plcc2fbc" "${g_plcc2fbc_args[@]}" "$FILE_FBC" >"$FILE_LOG" 2>"$WORK_DIR/stderr.log"
    exit_code=$?
    set -e

    if [[ "$exit_code" -eq 1 ]]; then
        log_error "plcc2fbc failed with a fatal error"
        if [[ -s "$WORK_DIR/stderr.log" ]]; then
            cat "$WORK_DIR/stderr.log" >&2
        fi
        exit 1
    fi
    if [[ "$exit_code" -ne 0 ]]; then
        log_info "Warning: plcc2fbc exited with exit code $exit_code"
    fi
}

# Renders $g_catalog_image with opm and populates g_catalog_packages with the
# sorted, unique set of package names carrying FBC lifecycle data. Aborts the
# run if opm fails (bad image ref, auth, network): a broken catalog fetch
# means no per-operator catalog claim can be trusted for this run.
fetch_catalog_packages() {
    [[ -z "$g_catalog_image" ]] && return

    log_info "Fetching catalog package list from $g_catalog_image..."
    local exit_code
    set +e
    opm render "$g_catalog_image" >"$WORK_DIR/catalog-render.json" 2>"$WORK_DIR/opm-stderr.log"
    exit_code=$?
    set -e

    if [[ "$exit_code" -ne 0 ]]; then
        log_error "opm render failed for catalog image $g_catalog_image"
        if [[ -s "$WORK_DIR/opm-stderr.log" ]]; then
            cat "$WORK_DIR/opm-stderr.log" >&2
        fi
        exit 1
    fi

    jq -r 'select(.schema == "io.openshift.operators.lifecycles.v1alpha1") | .package // empty' \
        "$WORK_DIR/catalog-render.json" | sort -u >"$FILE_CATALOG"

    g_catalog_packages=()
    while IFS= read -r name; do
        [[ -z "$name" ]] && continue
        g_catalog_packages+=("$name")
    done < "$FILE_CATALOG"

    # Extract lifecycle versions: package<TAB>MAJOR.MINOR from lifecycle entries.
    jq -r '
        select(.schema == "io.openshift.operators.lifecycles.v1alpha1")
        | (.package // empty) as $p
        | select($p != "")
        | (.versions[]?.name // empty) as $n
        | select($n != "")
        | "\($p)\t\($n)"
    ' "$WORK_DIR/catalog-render.json" | sort -u >"$FILE_CATALOG_LIFECYCLE_VERSIONS"

    # Extract bundle versions: package<TAB>MAJOR.MINOR from olm.bundle objects.
    # Truncate the full semver to MAJOR.MINOR for comparison.
    jq -r '
        select(.schema == "olm.bundle")
        | . as $b
        | ($b.properties[]? | select(.type == "olm.package") | .value.version // empty) as $v
        | select($v != "")
        | ($b.package // empty) as $p
        | select($p != "")
        | "\($p)\t\($v)"
    ' "$WORK_DIR/catalog-render.json" \
        | sed 's/\t\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\t\1.\2/' \
        | sort -u >"$FILE_CATALOG_BUNDLE_VERSIONS"
}

# Filters sorted package names from stdin to the selected operator set. In
# all-packages mode every name is retained. awk provides a portable set here;
# Bash associative arrays would break the script's Bash 3.2 compatibility.
_only_scoped_names() {
    if [[ -n "$g_operators_file" ]]; then
        awk 'NR == FNR { wanted[$0] = 1; next } ($0 in wanted)' "$FILE_REQUESTED" -
    else
        cat
    fi
}

# In "all packages" mode, derives g_operators from the run's own output
# (no -p flag means package names aren't known ahead of time; missing
# packages can't be detected in this mode). Requires the validation result
# arrays to already be populated.
_derive_operators_from_output() {
    [[ -n "$g_operators_file" ]] && return

    g_operators=()
    if $g_validate_only; then
        while IFS= read -r name; do
            [[ -n "$name" ]] && g_operators+=("$name")
        done < <(jq -r '.data[]?.package // empty' "$FILE_FBC" 2>/dev/null | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    else
        while IFS= read -r name; do
            [[ -n "$name" ]] && g_operators+=("$name")
        done < <(grep '^package:' "$FILE_FBC" 2>/dev/null | sed -e 's/^package:[[:space:]]*//' -e 's/^"//' -e 's/"$//')
    fi
    [[ ${#g_results_withissues[@]} -gt 0 ]] && g_operators+=("${g_results_withissues[@]}")
    [[ ${#g_results_duplicated[@]} -gt 0 ]] && g_operators+=("${g_results_duplicated[@]}")
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        local sorted=()
        while IFS= read -r name; do
            [[ -n "$name" ]] && sorted+=("$name")
        done < <(printf '%s\n' "${g_operators[@]}" | sort -u)
        g_operators=("${sorted[@]}")
    fi

    # A lifecycle entry served by the catalog but absent from current PLCC
    # data is stale catalog content. Include it in all-packages reports and
    # mark it as missing from PLCC.
    if [[ -n "$g_catalog_image" && ${#g_catalog_packages[@]} -gt 0 ]]; then
        : > "$FILE_PLCC_OPERATORS"
        if [[ ${#g_operators[@]} -gt 0 ]]; then
            printf '%s\n' "${g_operators[@]}" > "$FILE_PLCC_OPERATORS"
        fi
        while IFS= read -r name; do
            [[ -z "$name" ]] && continue
            g_results_missing+=("$name")
            g_operators+=("$name")
        done < <(awk 'FILENAME == ARGV[1] { plcc[$0] = 1; next } !($0 in plcc)' \
            "$FILE_PLCC_OPERATORS" "$FILE_CATALOG")

        local all_sorted=()
        while IFS= read -r name; do
            [[ -n "$name" ]] && all_sorted+=("$name")
        done < <(printf '%s\n' "${g_operators[@]}" | sort -u)
        g_operators=("${all_sorted[@]}")
    fi
}

# Classifies operator name "$1" into g_classify_result: "missing",
# "duplicated", "issues", or "passed".
_classify_operator() {
    local name="$1" m
    if [[ ${#g_results_missing[@]} -gt 0 ]]; then
        for m in "${g_results_missing[@]}"; do
            [[ "$m" == "$name" ]] && { g_classify_result="missing"; return; }
        done
    fi
    if [[ ${#g_results_duplicated[@]} -gt 0 ]]; then
        for m in "${g_results_duplicated[@]}"; do
            [[ "$m" == "$name" ]] && { g_classify_result="duplicated"; return; }
        done
    fi
    if [[ ${#g_results_withissues[@]} -gt 0 ]]; then
        for m in "${g_results_withissues[@]}"; do
            [[ "$m" == "$name" ]] && { g_classify_result="issues"; return; }
        done
    fi
    g_classify_result="passed"
}

# Computes the per-operator checks for name "$1" using the precomputed catalog
# status in "$2": g_mark_plcc, g_mark_catalog, and g_mark_done ("*" when
# every enabled check is "OK", for a quick at-a-glance scan; "" otherwise).
# The catalog check runs regardless of the PLCC outcome: a package can
# disappear from PLCC or fail validation while still being served from an
# older, stale catalog build, which is itself worth surfacing.
_check_marks() {
    local name="$1" catalog_status="$2"
    _classify_operator "$name"

    case "$g_classify_result" in
        missing) g_mark_plcc="MISSING" ;;
        passed) g_mark_plcc="OK" ;;
        duplicated) g_mark_plcc="DUPLICATE" ;;
        issues) g_mark_plcc="INVALID" ;;
    esac

    g_mark_catalog="-"
    if [[ -n "$g_catalog_image" ]]; then
        g_mark_catalog="$catalog_status"
    fi

    g_mark_done=""
    if [[ "$g_mark_plcc" == "OK" ]] && { [[ -z "$g_catalog_image" ]] || [[ "$g_mark_catalog" == "OK" ]]; }; then
        g_mark_done="*"
    fi
}

# Populates the result arrays from FILE_LOG/FILE_VAL and the run output.
collect_results() {
    # Missing operators: slog warnings about packages not found in PLCC data.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_missing+=("$name")
    done < <(jq -r 'select(.level == "WARN" and .msg == "requested package not found in PLCC data") | .package' "$FILE_LOG" 2>/dev/null)

    # Operators with validation issues: stderr JSONL entries with valid=false.
    # packageName is kept exactly as PLCC recorded it (may be a comma-separated
    # list for products not yet expanded into separate packages).
    g_results_issues="$(jq -s '[.[] | select(.valid == false and (.reasons | length) > 0)]' "$FILE_VAL" 2>/dev/null || echo '[]')"

    # REQ-VAL-01 (duplicate package name across products) is a catalog-level
    # rejection, mutually exclusive with per-product issues: a duplicate is
    # dropped before per-product validation ever runs, so it can't also carry
    # other reasons. Split it into its own bucket rather than lumping it into
    # the per-layer invalid result arrays.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_duplicated+=("$name")
    done < <(echo "$g_results_issues" | jq -r '.[] | select(any(.reasons[]; startswith("REQ-VAL-01"))) | .packageName' \
        | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | sort -u)

    # Build the unified set of invalid PLCC data entries, regardless of which
    # validation layer detected the problem. Scope it to the selected operator
    # file so a PLCC product carrying
    # "a,b" cannot count b when only a was requested.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_withissues+=("$name")
    done < <(echo "$g_results_issues" \
        | jq -r '.[] | select(any(.reasons[]; startswith("REQ-VAL-01")) | not) | .packageName' \
        | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | sort -u | _only_scoped_names)

    # Duplicate reports can also reference comma-separated PLCC products.
    # Restrict them after splitting for consistent selected-mode counts.
    local scoped_duplicates=()
    if [[ ${#g_results_duplicated[@]} -gt 0 ]]; then
        while IFS= read -r name; do
            [[ -n "$name" ]] && scoped_duplicates+=("$name")
        done < <(printf '%s\n' "${g_results_duplicated[@]}" | sort -u | _only_scoped_names)
    fi
    g_results_duplicated=()
    if [[ ${#scoped_duplicates[@]} -gt 0 ]]; then
        g_results_duplicated=("${scoped_duplicates[@]}")
    fi

    _derive_operators_from_output

    # Build catalog status once with awk, then retain an index-aligned status
    # array for O(1) reuse by every renderer.
    : > "$FILE_OPERATORS"
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        printf '%s\n' "${g_operators[@]}" > "$FILE_OPERATORS"
    fi
    if [[ -n "$g_catalog_image" ]]; then
        # Compute per-operator catalog status: OK, MISSING, or X/Y.
        # Input order matters: lifecycle versions, bundle versions, operators.
        awk -F'\t' '
            FILENAME == ARGV[1] { lifepkg[$1] = 1; lifecycle_mm[$1 SUBSEP $2] = 1; next }
            FILENAME == ARGV[2] { if (!seen[$1 SUBSEP $2]++) { bundle_total[$1]++; if (($1 SUBSEP $2) in lifecycle_mm) bundle_covered[$1]++ } ; next }
            {
                op = $0
                if (!(op in lifepkg)) { print "MISSING"; next }
                total = bundle_total[op] + 0; covered = bundle_covered[op] + 0
                if (total == 0 || covered == total) { print "OK" }
                else { print covered "/" total }
            }
        ' "$FILE_CATALOG_LIFECYCLE_VERSIONS" "$FILE_CATALOG_BUNDLE_VERSIONS" "$FILE_OPERATORS" \
            > "$FILE_CATALOG_STATUS"
    else
        awk '{ print "-" }' "$FILE_OPERATORS" > "$FILE_CATALOG_STATUS"
    fi
    while IFS= read -r status; do
        g_operator_catalog_statuses+=("$status")
    done < "$FILE_CATALOG_STATUS"

    if [[ ${#g_operators[@]} -gt 0 ]]; then
        local operator_index status
        for operator_index in "${!g_operators[@]}"; do
            name="${g_operators[$operator_index]}"
            status="${g_operator_catalog_statuses[$operator_index]}"
            _classify_operator "$name"
            if [[ "$status" == "MISSING" ]]; then
                g_results_notincatalog+=("$name")
            elif [[ "$status" == "OK" ]]; then
                g_results_catalogok+=("$name")
            elif [[ "$status" != "-" ]]; then
                g_results_catalogpartial+=("$name")
            fi
            if [[ "$g_classify_result" == "passed" ]]; then
                g_results_plccok+=("$name")
                if [[ -z "$g_catalog_image" || "$status" == "OK" ]]; then
                    g_results_allpassed+=("$name")
                fi
            fi
        done
    fi
}

print_operator_list() {
    log_info ""
    log_info "=== Requested operators ==="
    if [[ -n "$g_catalog_image" ]]; then
        log_info "$(printf "  %-1s  %-9s  %-9s  %s\n" " " "PLCC" "CATALOG" "OPERATOR")"
    else
        log_info "$(printf "  %-1s  %-9s  %s\n" " " "PLCC" "OPERATOR")"
    fi
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        local operator_index
        for operator_index in "${!g_operators[@]}"; do
            name="${g_operators[$operator_index]}"
            _check_marks "$name" "${g_operator_catalog_statuses[$operator_index]}"
            if [[ -n "$g_catalog_image" ]]; then
                log_info "$(printf "  %-1s  %-9s  %-9s  %s\n" \
                    "$g_mark_done" "$g_mark_plcc" "$g_mark_catalog" "$name")"
            else
                log_info "$(printf "  %-1s  %-9s  %s\n" \
                    "$g_mark_done" "$g_mark_plcc" "$name")"
            fi
        done
    fi
}

print_summary() {
    log_info ""
    log_info "=== Summary ==="
    local total=${#g_operators[@]}
    local missing_count=${#g_results_missing[@]}
    local duplicated_count=${#g_results_duplicated[@]}
    local issues_count=${#g_results_withissues[@]}
    local ok_count=${#g_results_plccok[@]}
    log_info "$(printf "  %-18s %d\n" "Total operators:" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC OK:" "$ok_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC DUPLICATE:" "$duplicated_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC INVALID:" "$issues_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC MISSING:" "$missing_count" "$total")"
    if [[ -n "$g_catalog_image" ]]; then
        local catalog_ok_count=${#g_results_catalogok[@]}
        local catalog_partial_count=${#g_results_catalogpartial[@]}
        local notincatalog_count=${#g_results_notincatalog[@]}
        local done_count=${#g_results_allpassed[@]}
        log_info "$(printf "  %-18s %d / %d\n" "CATALOG OK:" "$catalog_ok_count" "$total")"
        log_info "$(printf "  %-18s %d / %d\n" "CATALOG PARTIAL:" "$catalog_partial_count" "$total")"
        log_info "$(printf "  %-18s %d / %d\n" "CATALOG MISSING:" "$notincatalog_count" "$total")"
        log_info "$(printf "  %-18s %d / %d\n" "Fully done:" "$done_count" "$total")"
    fi
}

print_issues_detail() {
    log_info ""
    log_info "=== Validation issues detail ==="
    local json_issues_count
    json_issues_count="$(echo "$g_results_issues" | jq 'length')"
    if [[ "$json_issues_count" -eq 0 ]]; then
        log_info "  (none)"
    else
        log_info "$(echo "$g_results_issues" | jq --indent 2 -r '.[] | "  \(.packageName):", ("    " + (.reasons // [] | .[] | "- " + .))')"
    fi
}

print_csv_lists() {
    local missing_csv duplicated_csv issues_csv plcc_ok_csv
    local catalog_ok_csv catalog_partial_csv catalog_missing_csv fully_done_csv
    missing_csv="$(IFS=,; echo "${g_results_missing[*]:-}")"
    duplicated_csv="$(IFS=,; echo "${g_results_duplicated[*]:-}")"
    issues_csv="$(IFS=,; echo "${g_results_withissues[*]:-}")"
    plcc_ok_csv="$(IFS=,; echo "${g_results_plccok[*]:-}")"
    log_info ""
    log_info "=== CSV operator lists ==="
    log_info "- Missing:${missing_csv:+ $missing_csv}"
    log_info "- Duplicated:${duplicated_csv:+ $duplicated_csv}"
    log_info "- With issues:${issues_csv:+ $issues_csv}"
    log_info "- PLCC OK:${plcc_ok_csv:+ $plcc_ok_csv}"
    if [[ -n "$g_catalog_image" ]]; then
        catalog_ok_csv="$(IFS=,; echo "${g_results_catalogok[*]:-}")"
        catalog_partial_csv="$(IFS=,; echo "${g_results_catalogpartial[*]:-}")"
        catalog_missing_csv="$(IFS=,; echo "${g_results_notincatalog[*]:-}")"
        fully_done_csv="$(IFS=,; echo "${g_results_allpassed[*]:-}")"
        log_info "- Catalog OK:${catalog_ok_csv:+ $catalog_ok_csv}"
        log_info "- Catalog partial:${catalog_partial_csv:+ $catalog_partial_csv}"
        log_info "- Catalog missing:${catalog_missing_csv:+ $catalog_missing_csv}"
        log_info "- Fully done:${fully_done_csv:+ $fully_done_csv}"
    fi
}

# Copies one generated file into g_outdir and logs a summary entry.
_copy_one_file() {
    local file="$1" out="$2" msg="$3"
    if [[ ! -f "$file" ]]; then
        log_error "file $file not found"
        return
    fi
    cp -f "$file" "$out"
    log_info "$(printf "  %-24s %s" "$out" "$msg")"
}

copy_output_files() {
    local out_FBC msg_FBC
    local out_VAL="$g_outdir/validation.jsonl"  msg_VAL="Validation results"
    local out_LOG="$g_outdir/slog.json"         msg_LOG="Operational log"
    local out_SUM="$g_outdir/summary.txt"       msg_SUM="Summary"

    if $g_validate_only; then
        out_FBC="$g_outdir/plcc-dump.json"
        msg_FBC="Filtered PLCC data"
    else
        out_FBC="$g_outdir/fbc-output.yaml"
        msg_FBC="FBC blobs"
    fi

    log_info ""
    log_info "=== Generated files ==="
    _copy_one_file "$FILE_FBC" "$out_FBC" "$msg_FBC"
    _copy_one_file "$FILE_VAL" "$out_VAL" "$msg_VAL"
    _copy_one_file "$FILE_LOG" "$out_LOG" "$msg_LOG"
    if [[ -n "$g_catalog_image" ]]; then
        _copy_one_file "$FILE_CATALOG" "$g_outdir/catalog-packages.txt" "Catalog package list"
    fi
    # Log the summary destination before copying so summary.txt contains its
    # own generated-file entry and is byte-for-byte identical to stdout.
    log_info "$(printf "  %-24s %s" "$out_SUM" "$msg_SUM")"
    cp -f "$FILE_SUM" "$out_SUM"
}

# Returns success when the requested webhook sections contain "$1".
_webhook_has_section() {
    [[ ",$g_webhook_sections," == *",$1,"* ]]
}

# These helpers build record-separated Markdown chunks that stay below
# Slack's 3000-character section limit.
_webhook_chunks_start() {
    g_webhook_chunks_file="$1"
    g_webhook_chunk=""
    : > "$g_webhook_chunks_file"
}

_webhook_chunks_add() {
    local line="$1"
    if [[ -n "$g_webhook_chunk" ]] && (( ${#g_webhook_chunk} + ${#line} + 1 > 2800 )); then
        printf '%s\036' "$g_webhook_chunk" >> "$g_webhook_chunks_file"
        g_webhook_chunk=""
    fi
    [[ -n "$g_webhook_chunk" ]] && g_webhook_chunk+=$'\n'
    g_webhook_chunk+="$line"
}

_webhook_chunks_finish() {
    [[ -n "$g_webhook_chunk" ]] && printf '%s\036' "$g_webhook_chunk" >> "$g_webhook_chunks_file"
    return 0
}

_write_ready_operator_chunks() {
    local file="$1" name
    _webhook_chunks_start "$file"
    if [[ ${#g_results_allpassed[@]} -eq 0 ]]; then
        _webhook_chunks_add '_None_'
    else
        for name in "${g_results_allpassed[@]}"; do
            _webhook_chunks_add "- \`$name\`"
        done
    fi
    _webhook_chunks_finish
}

_write_requested_operator_chunks() {
    local file="$1" name line marker
    local max_name_length=0
    _webhook_chunks_start "$file"
    if [[ ${#g_operators[@]} -eq 0 ]]; then
        _webhook_chunks_add '_None_'
    else
        for name in "${g_operators[@]}"; do
            (( ${#name} > max_name_length )) && max_name_length=${#name}
        done
        local operator_index
        for operator_index in "${!g_operators[@]}"; do
            name="${g_operators[$operator_index]}"
            _check_marks "$name" "${g_operator_catalog_statuses[$operator_index]}"
            if [[ -n "$g_catalog_image" ]]; then
                if [[ "$g_mark_plcc" == "OK" && "$g_mark_catalog" == "OK" ]]; then
                    marker="✅"
                else
                    marker="  "
                fi
                printf -v line "%s  %-${max_name_length}s  PLCC: %-9s  Catalog: %s" \
                    "$marker" "$name" "$g_mark_plcc" "$g_mark_catalog"
            else
                printf -v line "%-${max_name_length}s  PLCC: %s" "$name" "$g_mark_plcc"
            fi
            _webhook_chunks_add "$line"
        done
    fi
    _webhook_chunks_finish
}

# Renders the payload from pre-built Markdown chunks and scalar result counts.
_render_webhook_payload() {
    local heading="$1" run_url="$2" ready_file="$3" operators_file="$4" payload_file="$5"
    local total=${#g_operators[@]}
    local missing_count=${#g_results_missing[@]}
    local duplicated_count=${#g_results_duplicated[@]}
    local issues_count=${#g_results_withissues[@]}
    local notincatalog_count=${#g_results_notincatalog[@]}
    local catalog_ok_count=${#g_results_catalogok[@]}
    local catalog_partial_count=${#g_results_catalogpartial[@]}

    jq -n \
        --arg heading "$heading" \
        --arg url "$run_url" \
        --arg scope "$([[ -n "$g_operators_file" ]] && echo 'Selected operators' || echo 'All operators')" \
        --argjson total "$total" \
        --argjson plcc_ok "${#g_results_plccok[@]}" \
        --argjson plcc_duplicate "$duplicated_count" \
        --argjson plcc_invalid "$issues_count" \
        --argjson plcc_missing "$missing_count" \
        --argjson catalog_ok "$catalog_ok_count" \
        --argjson catalog_partial "$catalog_partial_count" \
        --argjson catalog_missing "$notincatalog_count" \
        --argjson fully_done "${#g_results_allpassed[@]}" \
        --argjson has_catalog "$([[ -n "$g_catalog_image" ]] && echo true || echo false)" \
        --argjson show_summary "$(_webhook_has_section summary && echo true || echo false)" \
        --argjson show_list "$(_webhook_has_section list && echo true || echo false)" \
        --rawfile ready "$ready_file" \
        --rawfile operators "$operators_file" '
        def markdown_blocks($content):
          $content | split("\u001e") | map(select(length > 0) | {type: "section", text: {type: "mrkdwn", text: .}});
        def code_blocks($content):
          $content | split("\u001e") | map(select(length > 0) | {type: "section", text: {type: "mrkdwn", text: ("```\n" + . + "\n```")}});
        def status($label; $count):
          "• " + $label + ": \($count) / \($total)";
        def summary_markdown:
          ((if $has_catalog then ["*Ready in PLCC and catalog: \($fully_done) / \($total)*"] else [] end) + [
            "• Scope: \($scope)",
            "• Operators assessed: \($total)",
            status("PLCC valid"; $plcc_ok),
            status("PLCC duplicate"; $plcc_duplicate),
            status("PLCC invalid"; $plcc_invalid),
            status("PLCC missing"; $plcc_missing)
          ] + (if $has_catalog then [status("Catalog OK"; $catalog_ok), status("Catalog partial"; $catalog_partial), status("Catalog missing"; $catalog_missing)] else [] end)) | join("\n");
        {
          text: ($heading + ". " + $url),
          blocks: (
            [{type: "header", text: {type: "plain_text", text: $heading}}]
            + (if $show_summary then
                [{type: "section", text: {type: "mrkdwn", text: ("*Summary*\n" + summary_markdown)}}]
                + (if $has_catalog then [{type: "section", text: {type: "mrkdwn", text: "*Operators ready in PLCC and catalog*"}}] + markdown_blocks($ready) else [] end)
              else [] end)
            + (if $show_list then [{type: "section", text: {type: "mrkdwn", text: "*Requested operators*"}}] + code_blocks($operators) else [] end)
            + [{type: "section", text: {type: "mrkdwn", text: ("<" + $url + "|Open workflow run and download artifacts>")}}]
          )
        }' > "$payload_file"
}

# Writes a complete, non-secret Slack webhook payload. The GitHub Actions
# workflow owns the webhook URL and posts this file unchanged.
write_webhook_payload() {
    [[ -n "$g_webhook_sections" ]] || return 0

    local server_url="${GITHUB_SERVER_URL:-}"
    local repository="${GITHUB_REPOSITORY:-}"
    local run_id="${GITHUB_RUN_ID:-}"
    if [[ -z "$server_url" || -z "$repository" || -z "$run_id" ]]; then
        log_error "--webhook requires GITHUB_SERVER_URL, GITHUB_REPOSITORY, and GITHUB_RUN_ID"
        exit 1
    fi

    local heading="Operator lifecycle assessment"
    [[ -n "$g_operators_file" ]] && heading+=" ($(basename "$g_operators_file"))"
    local ready_file="$WORK_DIR/webhook-ready.txt"
    local operators_file="$WORK_DIR/webhook-operators.txt"
    _write_ready_operator_chunks "$ready_file"
    _write_requested_operator_chunks "$operators_file"
    _render_webhook_payload "$heading" \
        "${server_url}/${repository}/actions/runs/${run_id}" \
        "$ready_file" "$operators_file" "$g_outdir/slack-payload.json"
}

main() {
    WORK_DIR="$(mktemp -d)"
    FILE_FBC="$WORK_DIR/fbc.yaml"
    FILE_LOG="$WORK_DIR/slog.json"
    FILE_VAL="$WORK_DIR/validation.jsonl"
    FILE_SUM="$WORK_DIR/summary.txt"
    FILE_CATALOG="$WORK_DIR/catalog-packages.txt"
    FILE_CATALOG_LIFECYCLE_VERSIONS="$WORK_DIR/catalog-lifecycle-versions.txt"
    FILE_CATALOG_BUNDLE_VERSIONS="$WORK_DIR/catalog-bundle-versions.txt"
    FILE_REQUESTED="$WORK_DIR/requested-packages.txt"
    FILE_PLCC_OPERATORS="$WORK_DIR/plcc-operators.txt"
    FILE_OPERATORS="$WORK_DIR/operators.txt"
    FILE_CATALOG_STATUS="$WORK_DIR/catalog-status.txt"
    trap 'rm -rf "$WORK_DIR"' EXIT

    parse_args "$@"
    check_dependencies

    mkdir -p "$g_outdir"

    build_plcc2fbc
    run_plcc2fbc
    g_catalog_packages=()
    fetch_catalog_packages

    g_results_missing=()
    g_results_withissues=()
    g_results_duplicated=()
    g_results_notincatalog=()
    g_results_catalogok=()
    g_results_catalogpartial=()
    g_results_plccok=()
    g_results_allpassed=()
    g_operator_catalog_statuses=()
    g_results_issues=""
    collect_results

    print_operator_list
    print_summary
    print_issues_detail
    print_csv_lists

    copy_output_files
    write_webhook_payload
}

main "$@"
