#!/usr/bin/env bash
# The benchmark, in one command: ./bench/run.sh [label]  (default: <os>-<arch>)
#
# Measures native C libwebp (the ceiling) and the Go implementations in cmd/bench (this package,
# gen2brain/webp, gen2brain/vpx, golang.org/x/image/webp for decoding, eringen/gowebper) and
# writes results/<label>/: raw Go benchmark lines (latency.txt, throughput.txt, big.txt),
# quality scores, benchstat summaries, the soak series, binary sizes, and report.md.
#
# Method: every implementation runs in its own process; implementations are interleaved in
# rounds (A, B, C, A, B, C, ...); each process run holds the machine-wide lock $LOCK (so no two
# benchmarks share the machine) and starts only when the 1-minute load is under $LOADMAX; the
# same Go toolchain ($GOTOOLCHAIN) builds everything; GOMAXPROCS (or C's thread count) is the
# CPU count of the round. Quality is scored once (it does not depend on timing) through
# libwebp's dwebp.
#
# Environment: ROUNDS (10), CPUS ("1 2 4"), PHASES ("quality rounds big soak size report"),
# SOAK (50000 images), LOADMAX (half the cores), LOCK (/tmp/jotadrilo-bench.lock), FLOCK
# (flock), NOLOCK=1 (inside a container whose host holds the lock), ENC / DEC (regexes).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
cd "$here"
label=${1:-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)}
ROUNDS=${ROUNDS:-10}
CPUS=${CPUS:-"1 2 4"}
PHASES=${PHASES:-"quality rounds big soak size report"}
SOAK=${SOAK:-50000}
LOCK=${LOCK:-/tmp/jotadrilo-bench.lock}
FLOCK=${FLOCK:-flock}
ncpu=$(getconf _NPROCESSORS_ONLN)
LOADMAX=${LOADMAX:-$((ncpu / 2 > 1 ? ncpu / 2 : 1))}
ENC=${ENC:-.}
DEC=${DEC:-.}
export GOTOOLCHAIN=${GOTOOLCHAIN:-go1.27.4} GOFLAGS=-mod=mod CGO_ENABLED=0
out=$here/results/$label
work=${WORK:-$here/work}
bin=$work/bin
mkdir -p "$out" "$bin"
has() { [[ " $PHASES " == *" $1 "* ]]; }

locked() {
  if [[ ${NOLOCK:-0} == 1 ]]; then "$@"; else "$FLOCK" "$LOCK" "$@"; fi
}
load1() {
  if [[ -r /proc/loadavg ]]; then cut -d' ' -f1 /proc/loadavg; else sysctl -n vm.loadavg | awk '{print $2}'; fi
}
waitload() {
  for _ in $(seq 120); do
    awk -v l="$(load1)" -v m="$LOADMAX" 'BEGIN { exit !(l < m) }' && return 0
    sleep 5
  done
  echo "warning: 1-minute load $(load1) is still >= $LOADMAX; running anyway (recorded)" >&2
}

go build -o "$bin/report" ./cmd/report
if has quality || has rounds || has big; then
  echo "building (Go $GOTOOLCHAIN)" >&2
  go build -tags nodynamic -o "$bin/bench" ./cmd/bench # nodynamic: gen2brain/webp uses its WASM, never a system libwebp
  "$here/c/build.sh" "$bin/cbench" >/dev/null
  dwebp=$(BINDIR=$bin "$root/scripts/build-dwebp.sh")
  if [[ ! -f $work/in/.done ]]; then
    CWEBP=$bin/cwebp "$bin/bench" -prep "$work/in"
    touch "$work/in/.done"
  fi
  encs=$("$bin/bench" -list | awk '$1 == "enc" { print $2 }' | grep -E "$ENC" || true)
  decs=$("$bin/bench" -list | awk '$1 == "dec" { print $2 }' | grep -E "$DEC" || true)
  csettings="m0 m4 lossless"
  cpu_model=$( (sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name\|^CPU part' /proc/cpuinfo || uname -m) | sed 's/.*: //')
  {
  echo "date:     $(date -u +%Y-%m-%dT%H:%MZ)"
  echo "host:     $(uname -sm), $ncpu CPUs online, $cpu_model"
  echo "go:       $(go version)"
  echo "cc:       $(${CC:-cc} --version | head -1)"
  echo "libwebp:  $("$dwebp" -version | head -1)"
  echo "rounds:   $ROUNDS, CPUs: $CPUS, load gate: < $LOADMAX"
  } >> "$out/env.txt"
fi

in=$work/in
if has quality; then
  echo "quality" >&2
  : > "$out/quality-enc.jsonl"
  : > "$out/quality-dec.jsonl"
  for s in $csettings; do
    d=$work/out/c-libwebp-$s
    mkdir -p "$d"
    "$bin/cbench" "$in" latency enc "$s" 1 "$d"
    "$bin/bench" -in "$in" -score "$d" -quality "$dwebp" >> "$out/quality-enc.jsonl"
  done
  for e in $encs; do "$bin/bench" -in "$in" -enc "$e" -quality "$dwebp" >> "$out/quality-enc.jsonl"; done
  for d in $decs; do "$bin/bench" -in "$in" -dec "$d" -quality "$dwebp" >> "$out/quality-dec.jsonl"; done
fi

if has rounds; then
  for n in $CPUS; do
    for r in $(seq "$ROUNDS"); do
      waitload
      echo "# cpus $n round $r load $(load1) $(date -u +%H:%M:%S)" >> "$out/load.txt"
      echo "cpus $n round $r/$ROUNDS (load $(load1))" >&2
      for s in $csettings; do
        locked "$bin/cbench" "$in" latency enc "$s" "$n" >> "$out/latency.txt"
        locked "$bin/cbench" "$in" throughput enc "$s" "$n" >> "$out/throughput.txt"
      done
      locked "$bin/cbench" "$in" latency dec - "$n" >> "$out/latency.txt"
      locked "$bin/cbench" "$in" throughput dec - "$n" >> "$out/throughput.txt"
      for e in $encs; do
        GOMAXPROCS=$n locked "$bin/bench" -in "$in" -enc "$e" -latency >> "$out/latency.txt"
        GOMAXPROCS=$n locked "$bin/bench" -in "$in" -enc "$e" -throughput >> "$out/throughput.txt"
      done
      for d in $decs; do
        GOMAXPROCS=$n locked "$bin/bench" -in "$in" -dec "$d" -latency >> "$out/latency.txt"
        GOMAXPROCS=$n locked "$bin/bench" -in "$in" -dec "$d" -throughput >> "$out/throughput.txt"
      done
    done
  done
fi

if has big; then
  echo "24 MP" >&2
  for r in 1 2 3; do
    waitload
    for s in m0 lossless; do locked "$bin/cbench" "$in" big enc "$s" 1 >> "$out/big.txt"; done
    locked "$bin/cbench" "$in" big dec - 1 >> "$out/big.txt"
    for e in $(echo "$encs" | grep -E -- '-(m0|lossless)$' | grep -v gowebper); do
      GOMAXPROCS=1 locked "$bin/bench" -in "$in" -enc "$e" -big >> "$out/big.txt"
    done
    for d in $decs; do GOMAXPROCS=1 locked "$bin/bench" -in "$in" -dec "$d" -big >> "$out/big.txt"; done
  done
fi

if has soak; then
  echo "soak: $SOAK images" >&2
  mkdir -p "$work/soak"
  waitload
  (cd "$root" && WEBP_SOAK=$SOAK WEBP_SOAK_CSV=$out/soak.csv WEBP_SOAK_PROFILE=$work/soak \
    locked go test -count=1 -run '^TestSoak$' -timeout 3h . > "$out/soak.txt" 2>&1) || { cat "$out/soak.txt" >&2; exit 1; }
  if [[ -f $work/soak/heap-10000.pprof ]]; then
    go tool pprof -sample_index=inuse_space -top -base "$work/soak/heap-1000.pprof" "$work/soak/heap-10000.pprof" \
      > "$out/soak-heap-diff.txt" 2>&1 || true
  fi
fi

if has size; then
  echo "size" >&2
  locked "$here/size.sh" > "$out/size.md"
fi

if has report; then
  # benchstat (if installed) for confidence intervals and significance, C as the base column.
  if command -v benchstat >/dev/null; then
    grep -E '^BenchmarkEncode/' "$out/latency.txt" > "$work/enc.txt" || true
    grep -E '^BenchmarkDecode/' "$out/latency.txt" > "$work/dec.txt" || true
    benchstat -col /impl -filter '.unit:sec/op' "$work/enc.txt" > "$out/benchstat-encode.txt" 2>&1 || true
    benchstat -col /impl -filter '.unit:sec/op' "$work/dec.txt" > "$out/benchstat-decode.txt" 2>&1 || true
    benchstat -col /impl "$out/throughput.txt" > "$out/benchstat-throughput.txt" 2>&1 || true
  fi
  "$bin/report" "$out" > "$out/report.md"
  echo "wrote $out/report.md" >&2
fi
