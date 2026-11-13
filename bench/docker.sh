#!/usr/bin/env bash
# Optional extra, not used for the published numbers (those come from one native macOS
# environment, see run.sh). Runs the benchmark in Linux containers, one per CPU count, each limited to that many CPUs
# pinned to fixed cores (--cpus=N --cpuset-cpus=0..N-1) with GOMAXPROCS=N, all with the same Go
# toolchain as run.sh. The host's machine-wide lock is held for each container's whole run (a
# lock taken inside the VM would not exclude the host). Results go to results/<label>/, then
# the report is generated.
#   ./bench/docker.sh [platform linux/arm64] [cpu counts "1 2 4"]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
plat=${1:-linux/arm64}
cpus=${2:-"1 2 4"}
label=linux-${plat#linux/}-docker
LOCK=${LOCK:-/tmp/jotadrilo-bench.lock}
FLOCK=${FLOCK:-flock}
image=golang:1.27-bookworm
first=1
for n in $cpus; do
  phases="rounds"
  if [[ $first == 1 ]]; then phases="quality rounds big"; fi
  first=0
  echo "== $label: $n CPU(s), phases: $phases" >&2
  "$FLOCK" "$LOCK" docker run --rm --platform "$plat" --cpus="$n" --cpuset-cpus="0-$((n - 1))" --memory=6g \
    -v "$root":/src -v webp-bench-gomod:/go/pkg/mod -v webp-bench-gocache:/root/.cache/go-build \
    -e NOLOCK=1 -e PHASES="$phases" -e ROUNDS="${ROUNDS:-10}" \
    -e LOADMAX="${LOADMAX:-$n}" -e GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.4}" -e WORK=/src/bench/work-linux \
    -w /src/bench "$image" \
    bash -c 'export PATH=$PATH:/go/bin; go install golang.org/x/perf/cmd/benchstat@latest >/dev/null 2>&1 || true
      git config --global --add safe.directory "*"; ./run.sh '"$label"
done
# Report (and benchstat) over all CPU counts, on the host.
PHASES=report NOLOCK=1 "$here/run.sh" "$label"
