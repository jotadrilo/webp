#!/usr/bin/env bash
# Builds c/cbench natively against the pinned libwebp source, with libwebp's own SIMD dispatch
# (NEON on arm64, SSE2/SSE4.1 on amd64) and threads, at -O2 like libwebp's release builds.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
"$root/scripts/fetch-libwebp.sh" >&2
L=$root/third_party/libwebp/_src
out=${1:-$here/../work/bin/cbench}
mkdir -p "$(dirname "$out")"
cd "$L"
# shellcheck disable=SC2046
${CC:-cc} -O2 -w -DNDEBUG -DWEBP_USE_THREAD -I"$L/src" -I"$L" -o "$out" "$here/cbench.c" \
  $(ls src/dec/*.c src/enc/*.c src/dsp/*.c src/utils/*.c sharpyuv/*.c) -lpthread -lm
echo "$out"
