#!/usr/bin/env bash
# Moves one pinned input of `make generate` to its latest upstream release (or to the given
# version): updates internal/gen/toolchain.env, with each SHA-256 computed from the downloaded
# release, and the files that name the version. Does nothing when the pin is already there.
#   scripts/update-toolchain.sh libwebp|wasi-sdk|wasm2go [version]
# Versions are written as in toolchain.env: libwebp 1.6.0, wasi-sdk 34 (or 34.0), wasm2go v0.4.21.
# Prints key=value lines (tool, from, to, changed, notes) and appends them to $GITHUB_OUTPUT when
# set. Needs curl, shasum, tar, go, and gh (authenticated, or with GH_TOKEN) to find releases.
# It does not regenerate: run `make generate` (and, for libwebp, `make testdata`) afterwards.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
env_file=$root/internal/gen/toolchain.env
tool=${1:?usage: update-toolchain.sh libwebp|wasi-sdk|wasm2go [version]}
want=${2:-}
# shellcheck disable=SC1090
source "$env_file"
cache=$root/.cache/dl
mkdir -p "$cache"

latest_tag() { # repo, tag regex
  gh api "repos/$1/tags?per_page=100" --paginate --jq '.[].name' | grep -E "$2" | sort -V | tail -1
}
setvar() { # KEY VALUE: replace the KEY= line of toolchain.env
  local tmp
  tmp=$(mktemp)
  awk -v k="$1" -v v="$2" 'index($0, k "=") == 1 { print k "=" v; found = 1; next } { print }
    END { if (!found) exit 1 }' "$env_file" > "$tmp" || { rm -f "$tmp"; echo "no $1= in $env_file" >&2; exit 1; }
  cat "$tmp" > "$env_file"
  rm -f "$tmp"
}
subst() { # OLD NEW FILE...: replace every literal OLD with NEW
  local old=$1 new=$2
  shift 2
  OLD=$old NEW=$new perl -pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/g' "$@"
}
download() { # URL FILE: download (reusing a cached copy) and print its SHA-256
  [[ -f $2 ]] || { curl -fsSL --retry 3 -o "$2.part" "$1" && mv "$2.part" "$2"; }
  shasum -a 256 "$2" | cut -d' ' -f1
}

case $tool in
  libwebp)
    from=$LIBWEBP_VERSION
    to=${want:-$(latest_tag webmproject/libwebp '^v[0-9]+\.[0-9]+\.[0-9]+$')}
    to=${to#v}
    notes=https://chromium.googlesource.com/webm/libwebp/+/refs/tags/v$to/NEWS
    ;;
  wasi-sdk)
    from=${WASI_SDK_VERSION%%.*}
    if [[ -n $want ]]; then
      to=${want#wasi-sdk-}
      to=${to%%.*}
    else
      to=$(gh api "repos/WebAssembly/wasi-sdk/releases?per_page=50" \
        --jq '.[] | select(.draft | not) | select(.prerelease | not) | .tag_name' |
        grep -E '^wasi-sdk-[0-9]+$' | sort -V | tail -1)
      to=${to#wasi-sdk-}
    fi
    notes=https://github.com/WebAssembly/wasi-sdk/releases/tag/wasi-sdk-$to
    ;;
  wasm2go)
    from=$WASM2GO_VERSION
    to=${want:-$(latest_tag ncruces/wasm2go '^v[0-9]+\.[0-9]+\.[0-9]+$')}
    [[ $to == v* ]] || to=v$to
    notes=https://github.com/ncruces/wasm2go/releases/tag/$to
    ;;
  *)
    echo "unknown tool $tool (want libwebp, wasi-sdk or wasm2go)" >&2
    exit 2
    ;;
esac
[[ -n $to ]] || { echo "could not find the latest $tool release" >&2; exit 1; }

changed=false
if [[ $to != "$from" ]]; then
  changed=true
  case $tool in
    libwebp)
      url=https://storage.googleapis.com/downloads.webmproject.org/releases/webp/libwebp-$to.tar.gz
      tgz=$cache/libwebp-$to.tar.gz
      sum=$(download "$url" "$tgz")
      setvar LIBWEBP_VERSION "$to"
      setvar LIBWEBP_URL "$url"
      setvar LIBWEBP_SHA256 "$sum"
      # The committed license files must be the release's own (fetch-libwebp.sh checks them).
      tmp=$(mktemp -d)
      tar -xzf "$tgz" -C "$tmp" --strip-components=1 \
        "libwebp-$to/COPYING" "libwebp-$to/PATENTS" "libwebp-$to/AUTHORS"
      cp "$tmp/COPYING" "$root/third_party/libwebp/LICENSE"
      cp "$tmp/PATENTS" "$root/third_party/libwebp/PATENTS"
      cp "$tmp/AUTHORS" "$root/third_party/libwebp/AUTHORS"
      rm -rf "$tmp"
      subst "release $from." "release $to." "$root/NOTICE"
      subst "libwebp $from compiled" "libwebp $to compiled" "$root/NOTICE"
      ;;
    wasi-sdk)
      # The asset names carry the full version (wasi-sdk-34 ships wasi-sdk-34.0-<platform>).
      assets=$(gh api "repos/WebAssembly/wasi-sdk/releases/tags/wasi-sdk-$to" \
        --jq '.assets[] | .name + " " + (.digest // "-") + " " + .browser_download_url')
      full=$(sed -nE "s/^wasi-sdk-([0-9.]+)-x86_64-linux\.tar\.gz .*/\1/p" <<<"$assets")
      [[ -n $full ]] || { echo "wasi-sdk-$to has no x86_64-linux asset" >&2; exit 1; }
      for plat in arm64-macos x86_64-macos arm64-linux x86_64-linux; do
        name=wasi-sdk-$full-$plat.tar.gz
        read -r _ digest url < <(grep "^$name " <<<"$assets") ||
          { echo "wasi-sdk-$to has no $name" >&2; exit 1; }
        sum=$(download "$url" "$cache/$name")
        # Cross-check with the digest GitHub recorded at upload time, when it has one.
        if [[ $digest != - && $digest != "sha256:$sum" ]]; then
          echo "$name: downloaded SHA-256 $sum, GitHub says $digest" >&2
          exit 1
        fi
        setvar "WASI_SDK_SHA256_${plat//-/_}" "$sum"
      done
      setvar WASI_SDK_VERSION "$full"
      subst "releases/tag/wasi-sdk-$from" "releases/tag/wasi-sdk-$to" "$env_file"
      subst "wasi-sdk $from," "wasi-sdk $to," "$root/README.md"
      ;;
    wasm2go)
      # The Go checksum database vouches for the module; this fails if the version is unknown.
      (cd "$(mktemp -d)" && GOFLAGS='' go mod download -json "github.com/ncruces/wasm2go@$to" >/dev/null)
      setvar WASM2GO_VERSION "$to"
      subst "wasm2go $from " "wasm2go $to " "$root/README.md"
      ;;
  esac
fi

out="tool=$tool
from=$from
to=$to
changed=$changed
notes=$notes"
echo "$out"
if [[ -n ${GITHUB_OUTPUT:-} ]]; then
  echo "$out" >> "$GITHUB_OUTPUT"
fi
if [[ $changed == false ]]; then
  echo "$tool is already at the latest release ($from): nothing to do" >&2
fi
