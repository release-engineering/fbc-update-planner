#!/usr/bin/env bash
# Usage: scripts/plcc-lookup.sh <package-name>
# Fetches https://access.redhat.com/product-life-cycles/api/v2/products,
# finds the product(s) whose comma-separated "package" field contains the
# given name, and prints each version with its OCP compatibility.
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: $0 <package-name>" >&2
    exit 1
fi
pkg=$1

curl -sS 'https://access.redhat.com/product-life-cycles/api/v2/products' \
  | jq --arg pkg "$pkg" '
      .data[]
      | select(
          (.package // "")
          | split(",")
          | map(gsub("^\\s+|\\s+$"; ""))
          | any(. == $pkg)
        )
      | {
          product: .name,
          package: .package,
          versions: [
            .versions[] | {
              version: .name,
              openshift_compatibility: .openshift_compatibility
            }
          ]
        }
    '
