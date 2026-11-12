#!/usr/bin/env bash
# Installs the pinned wasi-sdk (checked by SHA-256) and wasm2go into .cache/ and prints their
# paths: the wasi-sdk directory and the wasm2go binary. Used by generate.sh.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck disable=SC1091
source "$root/internal/gen/toolchain.env"
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) plat=arm64-macos ;;
  Darwin-x86_64) plat=x86_64-macos ;;
  Linux-aarch64 | Linux-arm64) plat=arm64-linux ;;
  Linux-x86_64) plat=x86_64-linux ;;
  *) echo "unsupported host $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac
var=WASI_SDK_SHA256_${plat//-/_}
sum=${!var}
cache=$root/.cache
sdk=$cache/wasi-sdk-$WASI_SDK_VERSION-$plat
if [[ ! -x $sdk/bin/clang ]]; then
  mkdir -p "$cache/dl"
  tgz=$cache/dl/wasi-sdk-$WASI_SDK_VERSION-$plat.tar.gz
  if [[ ! -f $tgz ]] || ! echo "$sum  $tgz" | shasum -a 256 -c - >/dev/null 2>&1; then
    curl -fsSL -o "$tgz.part" \
      "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION%%.*}/wasi-sdk-$WASI_SDK_VERSION-$plat.tar.gz"
    mv "$tgz.part" "$tgz"
  fi
  echo "$sum  $tgz" | shasum -a 256 -c - >&2
  tar -xzf "$tgz" -C "$cache"
fi
bin=$cache/tools-$plat
if [[ ! -x $bin/wasm2go ]] || [[ $("$bin/wasm2go" -version) != "wasm2go $WASM2GO_VERSION" ]]; then
  GOBIN=$bin GOFLAGS=-mod=mod go install "github.com/ncruces/wasm2go@$WASM2GO_VERSION" >&2
fi
echo "$sdk $bin/wasm2go"
