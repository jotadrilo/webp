#!/usr/bin/env bash
# Regenerates the Go of internal/gen into a temporary directory and fails if it differs from
# the committed files: the committed Go is exactly what the pinned libwebp and toolchain produce.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
"$root/scripts/generate.sh" "$tmp"
status=0
for m in dec enc; do
  if ! cmp -s "$tmp/$m/libwebp.go" "$root/internal/gen/$m/libwebp.go"; then
    echo "internal/gen/$m/libwebp.go differs from what make generate produces:" >&2
    diff -u "$root/internal/gen/$m/libwebp.go" "$tmp/$m/libwebp.go" | head -40 >&2 || true
    status=1
  else
    echo "internal/gen/$m/libwebp.go is reproducible ($(shasum -a 256 "$tmp/$m/libwebp.go" | cut -d' ' -f1))"
  fi
done
exit $status
