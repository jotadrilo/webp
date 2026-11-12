#!/usr/bin/env bash
# Rebuilds the generated Go from the pinned official libwebp release and the pinned toolchain
# (internal/gen/toolchain.env, flags in internal/gen/flags.env): libwebp + internal/gen/csrc/*.c
# → two wasm32-wasi reactors (wasi-sdk clang), the decoder and the encoder, so that a program
# that only decodes does not link the encoder → Go (wasm2go). Reproducible byte for byte.
#   scripts/generate.sh [output dir]   (default internal/gen; writes dec/libwebp.go, enc/libwebp.go)
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$root/internal/gen}
"$root/scripts/fetch-libwebp.sh" >&2
read -r sdk wasm2go < <("$root/scripts/fetch-toolchain.sh")
# shellcheck disable=SC1091
source "$root/internal/gen/flags.env"
L=$root/third_party/libwebp/_src
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$L"
# The sources libwebp's own build compiles into libwebpdecoder and into libwebp's encoder and
# libsharpyuv. SIMD files compile to nothing without their target features; --gc-sections drops
# what the entry points do not reach.
dec_src=$(ls src/dec/*.c src/dsp/*.c src/utils/*.c | LC_ALL=C sort)
enc_src=$(ls src/enc/*.c src/dsp/*.c src/utils/*.c sharpyuv/*.c | LC_ALL=C sort)
build() { # name cflags ldflags exports sources...
  local name=$1 cflags=$2 ldflags=$3 exports=$4
  shift 4
  # shellcheck disable=SC2086
  "$sdk/bin/clang" --target=wasm32-wasip1 --sysroot="$sdk/share/wasi-sysroot" $cflags \
    -mexec-model=reactor -DNDEBUG -ffile-prefix-map="$L"=libwebp -ffile-prefix-map="$root"=. \
    -I"$L/src" -I"$L" -w -Wl,--no-entry -Wl,--gc-sections $ldflags \
    $(printf -- '-Wl,--export=%s ' $exports) \
    -o "$work/$name.wasm" "$root/internal/gen/csrc/$name.c" "$@"
  mkdir -p "$out/$name"
  # shellcheck disable=SC2086
  "$wasm2go" -pkg "$name" $WASM2GO_FLAGS -o "$work/$name.go" "$work/$name.wasm"
  mv "$work/$name.go" "$out/$name/libwebp.go"
  echo "wrote $out/$name/libwebp.go ($(wc -c < "$out/$name/libwebp.go" | tr -d ' ') bytes; wasm $(wc -c < "$work/$name.wasm" | tr -d ' ') bytes)" >&2
}
# shellcheck disable=SC2086
build dec "$DEC_CFLAGS" "$DEC_LDFLAGS" "malloc realloc free wp_features wp_decode" $dec_src
# shellcheck disable=SC2086
build enc "$ENC_CFLAGS" "$ENC_LDFLAGS" "malloc free wp_encode" $enc_src
