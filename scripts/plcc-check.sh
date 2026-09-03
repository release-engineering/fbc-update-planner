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
                     present in the given OCP catalog image. Off by default; requires opm.
  -h                 Show this help

Example usage:
./plcc-check.sh -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh -o \$(date +%y%m%d) > summary.txt
./plcc-check.sh --plcc -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators none -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators syntax -o \$(date +%y%m%d) top-operators > summary.txt
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
}

log_info() {
    echo -e "$@" | tee -a "$FILE_SUM"
}

log_error() {
    echo -e "Error: $@" >&2
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

    jq -r 'select(.schema == "io.openshift.operators.lifecycles.v1alpha1") | .package' \
        "$WORK_DIR/catalog-render.json" | sort -u >"$FILE_CATALOG"

    g_catalog_packages=()
    while IFS= read -r name; do
        [[ -z "$name" ]] && continue
        g_catalog_packages+=("$name")
    done < "$FILE_CATALOG"
}

# True (exit 0) if "$1" is present in g_catalog_packages.
in_catalog() {
    local name="$1" p
    if [[ ${#g_catalog_packages[@]} -gt 0 ]]; then
        for p in "${g_catalog_packages[@]}"; do
            [[ "$p" == "$name" ]] && return 0
        done
    fi
    return 1
}

# In "all packages" mode, derives g_operators from the run's own output
# (no -p flag means package names aren't known ahead of time; missing
# packages can't be detected in this mode). Requires g_results_withissues
# and g_results_duplicated to already be populated.
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

# Computes the per-operator checks for name "$1": g_mark_plcc ("OK",
# "DUPLICATE", "INVALID", or "MISSING"), g_mark_catalog ("OK"/"MISSING", or
# "-" when --catalog-image wasn't given), and g_mark_done ("*" when every
# enabled check is "OK", for a quick at-a-glance scan; "" otherwise);
# The catalog check runs regardless of the PLCC outcome: a package can
# disappear from PLCC or fail validation while still being served from an
# older, stale catalog build, which is itself worth surfacing.
_check_marks() {
    local name="$1"
    _classify_operator "$name"

    case "$g_classify_result" in
        missing) g_mark_plcc="MISSING" ;;
        passed) g_mark_plcc="OK" ;;
        duplicated) g_mark_plcc="DUPLICATE" ;;
        *) g_mark_plcc="INVALID" ;;
    esac

    g_mark_catalog="-"
    if [[ -n "$g_catalog_image" ]]; then
        if in_catalog "$name"; then
            g_mark_catalog="OK"
        else
            g_mark_catalog="MISSING"
        fi
    fi

    g_mark_done=""
    if [[ "$g_mark_plcc" == "OK" ]] && { [[ -z "$g_catalog_image" ]] || [[ "$g_mark_catalog" == "OK" ]]; }; then
        g_mark_done="*"
    fi
}

# Populates g_results_missing, g_results_issues, g_results_withissues,
# g_results_duplicated, g_operators, g_results_notincatalog, g_results_plccok,
# and g_results_allpassed from FILE_LOG/FILE_VAL and the run output.
collect_results() {
    # Missing operators: slog warnings about packages not found in PLCC data.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_missing+=("$name")
    done < <(jq -r 'select(.level == "WARN" and .msg == "requested package not found in PLCC data") | .package' "$FILE_LOG" 2>/dev/null)

    # Operators with validation issues: stderr JSONL entries with valid=false.
    # packageName is kept exactly as PLCC recorded it (may be a comma-separated
    # list for products not yet expanded into separate packages).
    g_results_issues="$(jq -s '[.[] | select((.reasons | length) > 0)]' "$FILE_VAL" 2>/dev/null || echo '[]')"

    # REQ-VAL-01 (duplicate package name across products) is a catalog-level
    # rejection, mutually exclusive with per-product issues: a duplicate is
    # dropped before per-product validation ever runs, so it can't also carry
    # other reasons. Split it into its own bucket rather than lumping it into
    # g_results_withissues.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_duplicated+=("$name")
    done < <(echo "$g_results_issues" | jq -r '.[] | select(any(.reasons[]; startswith("REQ-VAL-01"))) | .packageName' \
        | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | sort -u)

    # Build the set of individual operator names with (non-duplicate) issues,
    # splitting any comma-separated packageName so it lines up with individual
    # operator names.
    while IFS= read -r name; do
        [[ -n "$name" ]] && g_results_withissues+=("$name")
    done < <(echo "$g_results_issues" | jq -r '.[] | select(any(.reasons[]; startswith("REQ-VAL-01")) | not) | .packageName' \
        | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | sort -u)

    _derive_operators_from_output

    if [[ ${#g_operators[@]} -gt 0 ]]; then
        for name in "${g_operators[@]}"; do
            _classify_operator "$name"
            local is_in_catalog=true
            if [[ -n "$g_catalog_image" ]] && ! in_catalog "$name"; then
                is_in_catalog=false
                g_results_notincatalog+=("$name")
            fi
            if [[ "$g_classify_result" == "passed" ]]; then
                g_results_plccok+=("$name")
                if $is_in_catalog; then
                    g_results_allpassed+=("$name")
                fi
            fi
        done
    fi
}

print_operator_list() {
    local max_len=0
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        for name in "${g_operators[@]}"; do
            (( ${#name} > max_len )) && max_len=${#name}
        done
    fi

    log_info "\n=== Requested operators ==="
    if [[ -n "$g_catalog_image" ]]; then
        log_info "$(printf "  %-1s  %-9s  %-7s  %s\n" " " "PLCC" "CATALOG" "OPERATOR")"
    else
        log_info "$(printf "  %-1s  %-9s  %s\n" " " "PLCC" "OPERATOR")"
    fi
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        for name in "${g_operators[@]}"; do
            _check_marks "$name"
            if [[ -n "$g_catalog_image" ]]; then
                log_info "$(printf "  %-1s  %-9s  %-7s  %-${max_len}s\n" \
                    "$g_mark_done" "$g_mark_plcc" "$g_mark_catalog" "$name")"
            else
                log_info "$(printf "  %-1s  %-9s  %-${max_len}s\n" \
                    "$g_mark_done" "$g_mark_plcc" "$name")"
            fi
        done
    fi
}

print_summary() {
    log_info "\n=== Summary ==="
    local total=${#g_operators[@]}
    local missing_count=${#g_results_missing[@]}
    local duplicated_count=${#g_results_duplicated[@]}
    local issues_count=${#g_results_withissues[@]}
    local ok_count=$((total - missing_count - duplicated_count - issues_count))
    log_info "$(printf "  %-18s %d\n" "Total operators:" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC OK:" "$ok_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC DUPLICATE:" "$duplicated_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC INVALID:" "$issues_count" "$total")"
    log_info "$(printf "  %-18s %d / %d\n" "PLCC MISSING:" "$missing_count" "$total")"
    if [[ -n "$g_catalog_image" ]]; then
        local notincatalog_count=${#g_results_notincatalog[@]}
        local catalog_ok_count=$((total - notincatalog_count))
        local done_count=${#g_results_allpassed[@]}
        log_info "$(printf "  %-18s %d / %d\n" "CATALOG OK:" "$catalog_ok_count" "$total")"
        log_info "$(printf "  %-18s %d / %d\n" "CATALOG MISSING:" "$notincatalog_count" "$total")"
        log_info "$(printf "  %-18s %d / %d\n" "Fully done:" "$done_count" "$total")"
    fi
}

print_issues_detail() {
    log_info "\n=== Validation issues detail ==="
    local json_issues_count
    json_issues_count="$(echo "$g_results_issues" | jq 'length')"
    if [[ "$json_issues_count" -eq 0 ]]; then
        log_info "  (none)"
    else
        log_info "$(echo "$g_results_issues" | jq --indent 2 -r '.[] | "  \(.packageName):", ("    " + (.reasons // [] | .[] | "- " + .))')"
    fi
}

print_csv_lists() {
    log_info "\n=== CSV operator lists ==="
    log_info "- Missing: $(IFS=,; echo "${g_results_missing[*]:-}")"
    log_info "- Duplicated: $(IFS=,; echo "${g_results_duplicated[*]:-}")"
    log_info "- With issues: $(IFS=,; echo "${g_results_withissues[*]:-}")"
    log_info "- PLCC OK: $(IFS=,; echo "${g_results_plccok[*]:-}")"
    if [[ -n "$g_catalog_image" ]]; then
        log_info "- Catalog missing: $(IFS=,; echo "${g_results_notincatalog[*]:-}")"
        log_info "- Fully done: $(IFS=,; echo "${g_results_allpassed[*]:-}")"
    fi
}

# Copies one generated file into g_outdir and logs a summary entry.
_copy_one_file() {
    local file="$1" out="$2" msg="$3"
    if [[ ! -f "$file" ]]; then
        log_error "file $file not found"
    else
        cp -f "$file" "$out"
    fi
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

    log_info "\n === Generated files ==="
    _copy_one_file "$FILE_FBC" "$out_FBC" "$msg_FBC"
    _copy_one_file "$FILE_VAL" "$out_VAL" "$msg_VAL"
    _copy_one_file "$FILE_LOG" "$out_LOG" "$msg_LOG"
    if [[ -n "$g_catalog_image" ]]; then
        _copy_one_file "$FILE_CATALOG" "$g_outdir/catalog-packages.txt" "Catalog package list"
    fi
    _copy_one_file "$FILE_SUM" "$out_SUM" "$msg_SUM"
}

main() {
    WORK_DIR="$(mktemp -d)"
    FILE_FBC="$WORK_DIR/fbc.yaml"
    FILE_LOG="$WORK_DIR/slog.json"
    FILE_VAL="$WORK_DIR/validation.jsonl"
    FILE_SUM="$WORK_DIR/summary.txt"
    FILE_CATALOG="$WORK_DIR/catalog-packages.txt"
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
    g_results_plccok=()
    g_results_allpassed=()
    g_results_issues=""
    collect_results

    print_operator_list
    print_summary
    print_issues_detail
    print_csv_lists

    copy_output_files
}

main "$@"
