#!/usr/bin/env bash
# Downloads the pinned official libwebp release, checks its SHA-256 and unpacks it into
# third_party/libwebp/_src (ignored by git). Only LICENSE, PATENTS and AUTHORS are committed.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck disable=SC1091
source "$root/internal/gen/toolchain.env"
dest=$root/third_party/libwebp/_src
stamp=$dest/.sha256
if [[ -f $stamp && $(cat "$stamp") == "$LIBWEBP_SHA256" ]]; then
  exit 0
fi
cache=$root/.cache/dl
mkdir -p "$cache"
tgz=$cache/libwebp-$LIBWEBP_VERSION.tar.gz
if [[ ! -f $tgz ]] || ! echo "$LIBWEBP_SHA256  $tgz" | shasum -a 256 -c - >/dev/null 2>&1; then
  curl -fsSL -o "$tgz.part" "$LIBWEBP_URL"
  mv "$tgz.part" "$tgz"
fi
echo "$LIBWEBP_SHA256  $tgz" | shasum -a 256 -c -
rm -rf "$dest"
mkdir -p "$dest"
tar -xzf "$tgz" -C "$dest" --strip-components=1
# The committed license files must be the release's own.
for f in COPYING:LICENSE PATENTS:PATENTS AUTHORS:AUTHORS; do
  cmp "$dest/${f%%:*}" "$root/third_party/libwebp/${f##*:}"
done
echo "$LIBWEBP_SHA256" > "$stamp"
echo "libwebp $LIBWEBP_VERSION in $dest"
