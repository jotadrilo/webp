#!/usr/bin/env bash
# Regenerates internal/gen/libwebp.go into a temporary file and fails if it differs from the
# committed one: the committed Go is exactly what the pinned libwebp and toolchain produce.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
"$root/scripts/generate.sh" "$tmp/libwebp.go"
if ! cmp -s "$tmp/libwebp.go" "$root/internal/gen/libwebp.go"; then
  echo "internal/gen/libwebp.go differs from what make generate produces:" >&2
  diff -u "$root/internal/gen/libwebp.go" "$tmp/libwebp.go" | head -40 >&2 || true
  exit 1
fi
echo "internal/gen/libwebp.go is reproducible ($(shasum -a 256 "$tmp/libwebp.go" | cut -d' ' -f1))"
