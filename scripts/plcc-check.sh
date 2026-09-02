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
  -h                 Show this help

Example usage:
./plcc-check.sh -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh -o \$(date +%y%m%d) > summary.txt
./plcc-check.sh --plcc -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators none -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --validators syntax -o \$(date +%y%m%d) top-operators > summary.txt
EOF
}

# Globals populated by parse_args() and consumed throughout the script.
g_outdir="."
g_input_file=""
g_validate_only=false
g_plcc_validators=""
g_operators_file=""

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

# Populates g_results_missing, g_results_issues, g_results_withissues,
# g_results_duplicated, g_operators, and g_results_passed from FILE_LOG/
# FILE_VAL and the run output.
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
            if [[ "$g_classify_result" == "passed" ]]; then
                g_results_passed+=("$name")
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
    if [[ ${#g_operators[@]} -gt 0 ]]; then
        for name in "${g_operators[@]}"; do
            _classify_operator "$name"
            case "$g_classify_result" in
                missing) log_info "$(printf "  ✗  %-${max_len}s  [NOT FOUND]\n" "$name")" ;;
                duplicated) log_info "$(printf "  ≡  %-${max_len}s  [DUPLICATED]\n" "$name")" ;;
                issues) log_info "$(printf "  !  %-${max_len}s  [WITH ISSUES]\n" "$name")" ;;
                *) log_info "$(printf "  ✓  %s\n" "$name")" ;;
            esac
        done
    fi
}

print_summary() {
    log_info "\n=== Summary ==="
    local total=${#g_operators[@]}
    local missing_count=${#g_results_missing[@]}
    local duplicated_count=${#g_results_duplicated[@]}
    local issues_count=${#g_results_withissues[@]}
    local passed_count=$((total - missing_count - duplicated_count - issues_count))
    log_info "$(printf "  %-14s %d\n" "Total:" "$total")"
    log_info "$(printf "  %-14s %d\n" "Passed:" "$passed_count")"
    log_info "$(printf "  %-14s %d\n" "Not found:" "$missing_count")"
    log_info "$(printf "  %-14s %d\n" "Duplicated:" "$duplicated_count")"
    log_info "$(printf "  %-14s %d\n" "With issues:" "$issues_count")"
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
    log_info "- Passed: $(IFS=,; echo "${g_results_passed[*]:-}")"
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
    _copy_one_file "$FILE_SUM" "$out_SUM" "$msg_SUM"
}

main() {
    WORK_DIR="$(mktemp -d)"
    FILE_FBC="$WORK_DIR/fbc.yaml"
    FILE_LOG="$WORK_DIR/slog.json"
    FILE_VAL="$WORK_DIR/validation.jsonl"
    FILE_SUM="$WORK_DIR/summary.txt"
    trap 'rm -rf "$WORK_DIR"' EXIT

    parse_args "$@"
    check_dependencies

    mkdir -p "$g_outdir"

    build_plcc2fbc
    run_plcc2fbc

    g_results_missing=()
    g_results_withissues=()
    g_results_duplicated=()
    g_results_passed=()
    g_results_issues=""
    collect_results

    print_operator_list
    print_summary
    print_issues_detail
    print_csv_lists

    copy_output_files
}

main "$@"
