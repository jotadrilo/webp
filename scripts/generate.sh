#!/usr/bin/env bash
# Rebuilds internal/gen/libwebp.go from the pinned official libwebp release and the pinned
# toolchain (internal/gen/toolchain.env): libwebp + internal/gen/csrc/shim.c → wasm32-wasi
# reactor (wasi-sdk clang) → Go (wasm2go). The output is reproducible byte for byte on any host.
#   scripts/generate.sh [output.go]   (default internal/gen/libwebp.go)
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$root/internal/gen/libwebp.go}
"$root/scripts/fetch-libwebp.sh" >&2
read -r sdk wasm2go < <("$root/scripts/fetch-toolchain.sh")
# shellcheck disable=SC1091
source "$root/internal/gen/flags.env"
L=$root/third_party/libwebp/_src
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$L"
# The sources libwebp's own build compiles into libwebp (decoder + encoder) and libsharpyuv.
# SIMD files compile to nothing without their target features.
src=$(ls src/dec/*.c src/enc/*.c src/dsp/*.c src/utils/*.c sharpyuv/*.c | LC_ALL=C sort)
exports=$(printf -- '-Wl,--export=%s ' malloc realloc free wp_encode wp_features wp_decode)
# shellcheck disable=SC2086
"$sdk/bin/clang" --target=wasm32-wasip1 --sysroot="$sdk/share/wasi-sysroot" $CFLAGS \
  -mexec-model=reactor -DNDEBUG -ffile-prefix-map="$L"=libwebp -ffile-prefix-map="$root"=. \
  -I"$L/src" -I"$L" -w -Wl,--no-entry -Wl,--gc-sections $LDFLAGS $exports \
  -o "$work/libwebp.wasm" "$root/internal/gen/csrc/shim.c" $src
# shellcheck disable=SC2086
"$wasm2go" -pkg gen $WASM2GO_FLAGS -o "$work/libwebp.go" "$work/libwebp.wasm"
mv "$work/libwebp.go" "$out"
echo "wrote $out ($(wc -c < "$out") bytes; wasm $(wc -c < "$work/libwebp.wasm") bytes)" >&2
