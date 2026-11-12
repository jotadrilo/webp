#!/usr/bin/env bash
# Binary size and clean-cache build time of a minimal program per library and operation, as the
# delta over a baseline program that uses only the standard image package. CGO_ENABLED=0,
# -trimpath, default tags (what a user gets). Prints a Markdown table.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$here"
export CGO_ENABLED=0
prog() {
  case $1 in
  baseline) echo 'import "image"; func main() { println(image.NewNRGBA(image.Rect(0, 0, 1, 1)) != nil) }' ;;
  ours-decode) echo 'import ("bytes"; "github.com/jotadrilo/webp/decode"); func main() { m, _ := decode.Decode(bytes.NewReader(nil)); println(m == nil) }' ;;
  ours-encode) echo 'import ("image"; "io"; "github.com/jotadrilo/webp/encode"); func main() { println(encode.Encode(io.Discard, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil) == nil) }' ;;
  ours-both) echo 'import ("bytes"; "image"; "io"; "github.com/jotadrilo/webp"); func main() { m, _ := webp.Decode(bytes.NewReader(nil)); println(m == nil, webp.Encode(io.Discard, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil) == nil) }' ;;
  gen2brain-decode) echo 'import ("bytes"; "github.com/gen2brain/webp"); func main() { m, _ := webp.Decode(bytes.NewReader(nil)); println(m == nil) }' ;;
  gen2brain-encode) echo 'import ("image"; "io"; "github.com/gen2brain/webp"); func main() { println(webp.Encode(io.Discard, image.NewNRGBA(image.Rect(0, 0, 1, 1))) == nil) }' ;;
  vpx-decode) echo 'import ("bytes"; "github.com/gen2brain/vpx/webp"); func main() { m, _ := webp.Decode(bytes.NewReader(nil)); println(m == nil) }' ;;
  vpx-encode) echo 'import ("image"; "io"; "github.com/gen2brain/vpx/webp"); func main() { println(webp.Encode(io.Discard, image.NewNRGBA(image.Rect(0, 0, 1, 1))) == nil) }' ;;
  ximage-decode) echo 'import ("bytes"; "golang.org/x/image/webp"); func main() { m, _ := webp.Decode(bytes.NewReader(nil)); println(m == nil) }' ;;
  gowebper-encode) echo 'import ("image"; "io"; "github.com/eringen/gowebper"); func main() { println(gowebper.Encode(io.Discard, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil) == nil) }' ;;
  esac
}
order=(baseline ours-decode ours-encode ours-both gen2brain-decode gen2brain-encode vpx-decode vpx-encode ximage-decode gowebper-encode)
build() { # name -> "bytes seconds" (the faster of two builds, each with an empty build cache)
  local a b
  a=$(build1 "$1")
  b=$(build1 "$1")
  python3 -c "a='$a'.split(); b='$b'.split(); print(a[0], min(float(a[1]), float(b[1])))"
}
build1() {
  mkdir -p "$here/work/size/$1"
  echo "package main; $(prog "$1")" | gofmt > "$here/work/size/$1/main.go"
  local cache t0 t1
  cache=$(mktemp -d "$work/cache-XXXX")
  t0=$(python3 -c "import time;print(time.time())")
  GOCACHE=$cache GOFLAGS=-mod=mod go build -trimpath -o "$work/$1" "./work/size/$1"
  t1=$(python3 -c "import time;print(time.time())")
  echo "$(wc -c < "$work/$1" | tr -d ' ') $(python3 -c "print(round($t1-$t0,1))")"
  rm -rf "$cache"
}
read -r base_size base_time < <(build baseline)
echo "| program | binary | over baseline | clean build s | over baseline |"
echo "|---|---:|---:|---:|---:|"
printf '| baseline (image only) | %.1f MB | – | %s | – |\n' "$(python3 -c "print($base_size/1e6)")" "$base_time"
for n in "${order[@]:1}"; do
  read -r s t < <(build "$n")
  printf '| %s | %.1f MB | +%.2f MB | %s | +%.1f |\n' "$n" "$(python3 -c "print($s/1e6)")" \
    "$(python3 -c "print(($s-$base_size)/1e6)")" "$t" "$(python3 -c "print($t-$base_time)")"
done
