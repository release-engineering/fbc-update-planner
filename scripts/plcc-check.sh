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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [options] <operators-file>

Run plcc2fbc against a list of operators and summarize results.

Arguments:
  <operators-file>   File with one operator name per line (blank lines and
                     lines starting with # are ignored)

Options:
  -o <dir>           Output directory for generated files (default: current directory)
  --plcc             Validate PLCC data only (skip FBC generation)
  -h                 Show this help

Example usage:
./plcc-check.sh -o \$(date +%y%m%d) top-operators > summary.txt
./plcc-check.sh --plcc -o \$(date +%y%m%d) top-operators > summary.txt
EOF
}

outdir="."
validate_only=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) outdir="$2"; shift 2 ;;
        --plcc) validate_only=true; shift ;;
        -h) usage; exit 0 ;;
        -*) usage >&2; exit 1 ;;
        *) break ;;
    esac
done

if [[ $# -ne 1 ]]; then
    usage >&2
    exit 1
fi

operators_file="$1"

if [[ ! -f "$operators_file" ]]; then
    echo "Error: operators file not found: $operators_file" >&2
    exit 1
fi

if ! command -v jq &>/dev/null; then
    echo "Error: jq is required but not found in PATH" >&2
    exit 1
fi

mkdir -p "$outdir"

# Read operator names, skipping blank lines and comments.
operators=()
while IFS= read -r line; do
    line="${line%%#*}"
    line="$(echo "$line" | xargs)"
    [[ -z "$line" ]] && continue
    operators+=("$line")
done < "$operators_file"

if [[ ${#operators[@]} -eq 0 ]]; then
    echo "Error: no operator names found in $operators_file" >&2
    exit 1
fi

pkg_list="$(IFS=,; echo "${operators[*]}")"

echo "Building plcc2fbc..."
make -C "$ROOT_DIR" build --quiet

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fbc_out="$tmpdir/fbc.yaml"
log_out="$tmpdir/slog.json"
val_out="$tmpdir/validation.jsonl"

plcc2fbc_args=(--allow-missing -o yaml -l "$val_out" -p "$pkg_list")
if $validate_only; then
    plcc2fbc_args+=(--dump-plcc)
    echo "Running plcc2fbc with ${#operators[@]} operators (PLCC validation only)..."
else
    echo "Running plcc2fbc with ${#operators[@]} operators..."
fi
set +e
"$ROOT_DIR/bin/plcc2fbc" "${plcc2fbc_args[@]}" "$fbc_out" >"$log_out" 2>"$tmpdir/stderr.log"
exit_code=$?
set -e

if [[ "$exit_code" -eq 1 ]]; then
    echo "Error: plcc2fbc failed with a fatal error" >&2
    if [[ -s "$tmpdir/stderr.log" ]]; then
        cat "$tmpdir/stderr.log" >&2
    fi
    exit 1
fi

# --- Parse results ---

# Missing operators: slog warnings about packages not found in PLCC data.
missing=()
while IFS= read -r name; do
    [[ -n "$name" ]] && missing+=("$name")
done < <(jq -r 'select(.msg == "requested package not found in PLCC data") | .package' "$log_out" 2>/dev/null)

# Operators with validation issues: stderr JSONL entries with valid=false.
issues_json="$(jq -s '[.[] | select((.reasons | length) > 0)]' "$val_out" 2>/dev/null || echo '[]')"

# Build a set of package names that have issues.
issue_names=()
while IFS= read -r name; do
    [[ -n "$name" ]] && issue_names+=("$name")
done < <(echo "$issues_json" | jq -r '.[].packageName' | sort -u)

# Compute max operator name length for aligned output.
max_len=0
for name in "${operators[@]}"; do
    (( ${#name} > max_len )) && max_len=${#name}
done

# --- Copy output files ---
if $validate_only; then
    if [[ -f "$fbc_out" ]]; then
        cp -f "$fbc_out" "$outdir/plcc-dump.json"
    else
        echo "Warning: no PLCC dump file produced (exit code $exit_code)" >&2
    fi
else
    if [[ -f "$fbc_out" ]]; then
        cp -f "$fbc_out" "$outdir/fbc-output.yaml"
    else
        echo "Warning: no FBC output file produced (exit code $exit_code)" >&2
    fi
fi
cp -f "$val_out" "$outdir/validation.jsonl"
cp -f "$log_out" "$outdir/slog.json"

# --- Print summary ---

echo ""
echo "=== Requested operators ==="
for name in "${operators[@]}"; do
    is_missing=false
    for m in "${missing[@]}"; do
        [[ "$m" == "$name" ]] && is_missing=true && break
    done
    has_issues=false
    for m in "${issue_names[@]}"; do
        [[ "$m" == "$name" ]] && has_issues=true && break
    done

    if $is_missing; then
        printf "  ✗  %-${max_len}s  [NOT FOUND]\n" "$name"
    elif $has_issues; then
        printf "  !  %-${max_len}s  [WITH ISSUES]\n" "$name"
    else
        printf "  ✓  %s\n" "$name"
    fi
done

echo ""
echo "=== Summary ==="
total=${#operators[@]}
missing_count=${#missing[@]}
issues_count=${#issue_names[@]}
passed_count=$((total - missing_count - issues_count))
printf "  %-14s %d\n" "Total:" "$total"
printf "  %-14s %d\n" "Passed:" "$passed_count"
printf "  %-14s %d\n" "Not found:" "$missing_count"
printf "  %-14s %d\n" "With issues:" "$issues_count"

echo ""
echo "=== Validation issues detail ==="
json_issues_count="$(echo "$issues_json" | jq 'length')"
if [[ "$json_issues_count" -eq 0 ]]; then
    echo "  (none)"
else
    echo "$issues_json" | jq -r '.[] | "  \(.packageName):", ("    " + (.reasons // [] | .[] | "- " + .))'
fi

echo ""
echo "Output files saved to: $outdir/"
if $validate_only; then
    echo "  plcc-dump.json       Filtered PLCC data"
else
    echo "  fbc-output.yaml      FBC blobs"
fi
echo "  validation.jsonl     Validation results"
echo "  slog.json            Operational log"
