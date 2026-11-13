# Benchmarks

> **Provisional.** Every number here was measured on a shared Mac (Apple M5 Pro, 18 cores)
> while other work was running. The method below limits the damage (a machine-wide lock,
> interleaved rounds, a load gate, benchstat over ten rounds), but the comparison tables against
> other implementations are **pending a quiet-machine pass** in the environment below, which
> will replace all of this.

## Method

The libraries are compared on their own, in **one environment, repeated the same way every
time: native macOS on one Mac**, with no containers or VMs. (`bench/docker.sh` runs the same
harness in pinned Linux containers. It is an optional extra and its numbers are not
published.) The command is

```sh
./bench/run.sh            # = make bench; label macos-arm64; ROUNDS=10 CPUS="1 2 4"
```

and `bench/results/<label>/env.txt` records what a later run must match: the Mac model
(`hw.model`), the chip and its core counts per performance level (`hw.perflevel0/1`), memory,
the macOS version and build (`sw_vers`), power source, charge and power mode (`pmset -g batt`,
`pmset -g`), the process priority, the load average before the run (and `load.txt` the load at
each round), the Go and C compiler versions, this repository's commit, and every compared
library's version. The processes run at the default priority with no QoS clamp, so macOS keeps
them on performance cores like any foreground work. They never run under `taskpolicy -c
utility/background`. Run as root, the script raises them to `nice -5`. CPU counts 1, 2 and 4 are
set through `GOMAXPROCS` (for the C harness, its thread count, in one process).

- **Implementations:** native C libwebp 1.6.0 (`bench/c/cbench.c`, the ceiling, built from the
  same pinned source with its NEON/SSE2 code and threads), this package, gen2brain/webp v0.6.4
  (built with `-tags nodynamic`, so that it always uses its WASM and never a system libwebp),
  gen2brain/vpx v0.2.2, golang.org/x/image/webp v0.47.0 (decoder only), and eringen/gowebper
  v0.1.2 (lossless encoder only).
- **Settings:** lossy quality 80 at method 0 (thumbnails) and 4 (libwebp's default), and
  lossless at each package's default effort.
- **Inputs:** seven synthetic 384 px thumbnails (`internal/testimg`: photo-like, flat, line art,
  gradient, noise, and two with alpha) and a 24 MP photo-like image. Decoders get the
  thumbnails encoded by libwebp's `cwebp`, lossy and lossless.
- **Isolation:** each implementation runs in its own process, so its allocations, GC and peak
  RSS are its own. Implementations are interleaved in rounds (C, A, B, …, C, A, B, …; ten
  rounds). Every process run holds the machine-wide lock `/tmp/jotadrilo-bench.lock`, so no two
  benchmarks share the machine, and starts only when the 1-minute load is below half the cores.
  One Go toolchain (`GOTOOLCHAIN=go1.27.4`) builds everything. The load, CPU model and versions
  are recorded with each run.
- **Per CPU count N:** single-image latency with `GOMAXPROCS=N` (C: N threads allowed within one
  image), so an implementation that parallelizes one image gains here. Throughput runs N
  concurrent workers for one second; scaling is that throughput over N times the 1-CPU one.
  Implementations that use more than one core per image: gen2brain/vpx lossy encoding (its
  default `Threads: 0` = GOMAXPROCS) and C libwebp with threads. This package, gen2brain/webp,
  x/image and gowebper use one.
- **Metrics:** median over rounds of each image's mean time, p50/p95/p99, bytes and objects
  allocated per operation, GC pause per operation, peak RSS, output bytes, SSIM (luma) and PSNR
  (RGB), both composited over white and scored through libwebp's `dwebp`, whether alpha is kept
  exactly, and for decoders, the difference from `dwebp`'s pixels. Ratios are to C libwebp
  measured in the same run. `benchstat` gives confidence intervals and significance
  (`bench/results/<label>/benchstat-*.txt`).

## Results

*Pending the quiet-machine pass* (`bench/results/macos-arm64/report.md` and `env.txt`).

Reference environment (for the pass): Mac17,9, Apple M5 Pro (6 Super + 12 Performance cores,
48 GB), macOS 26.3.2 (25D2150), on AC power at 100 %, power mode 0 (automatic, no low-power
mode), Go 1.27.4, gen2brain/webp v0.6.4, gen2brain/vpx v0.2.2, golang.org/x/image v0.47.0,
eringen/gowebper v0.1.2, C libwebp 1.6.0.

An earlier exploratory run on the same Mac (the prototype this package started from, without
the lock, mean over nine thumbnails, method 0, q80) gave C libwebp 1.85 ms, gen2brain/vpx
2.73 ms, the prototype 3.92 ms (47 KB allocated per encode) and gen2brain/webp 5.18 ms
(5.6 MB). The package's own `go test -bench` (one 384 × 256 photo, `GOMAXPROCS=1`) now gives
2.1 ms (method 0), 11.8 ms (method 4), 1.06 ms (lossy decode) and 1.3 ms (lossless decode),
with **0 B allocated per encode** and only the image per decode.

## Memory

### Soak: thousands of images

`TestSoak` encodes and decodes mixed images (1 px to 640 px, every kind, with and without
alpha, lossy and lossless) with concurrent bursts of 4 × GOMAXPROCS goroutines, and samples
the heap after a GC. It fails if the heap or the peak RSS grows after the warm-up. CI runs 2000
images on every push; `bench/run.sh` runs 50,000 and saves the heap profile diff between
image 1000 and image 10,000 (`bench/results/<label>/soak-heap-diff.txt`). The `goleak` check
in `TestMain` fails if any goroutine is left behind.

The soak found one real problem, now fixed. The pool was a FIFO channel, so sequential calls
rotated through every idle instance, and each grew to fit the largest image. The heap went from
25 MB to 184 MB over 2000 images and kept climbing. As a stack (LIFO), sequential calls reuse
one warm instance and the others go cold and are released 10 s after their last use:

| images | 100 | 200 (after a burst of 72 goroutines) | 500 | 1000 | 1500 | 2000 |
|---|---:|---:|---:|---:|---:|---:|
| FIFO pool: heap in use MB | 24.7 | – | 24.7 | 117.5 (burst) | 167.7 | 184.3 |
| LIFO pool: heap in use MB | 24.7 | 87.5 | 87.5 | 87.5 | 87.5 | 87.5 |
| LIFO pool: heap objects | 0.7k | 2.6k | 2.6k | 2.7k | 2.6k | 2.7k |

(18 cores: after the burst the pool keeps up to 18 encoder and 18 decoder instances; they are
released once idle.) The 50,000-image series is pending the benchmark window.

### One 24 MP image

Peak RSS of the process above its baseline (the 96 MB RGBA source of an encode is in the
baseline), 1 CPU, one run each (RSS does not depend on the machine's load):

| operation | C libwebp | this package, first version | after the changes below |
|---|---:|---:|---:|
| lossy encode, method 0 | 39 MB | 237 MB | 152 MB, then strip import (pending) |
| lossless encode | 711 MB | 3071 MB | 759 MB |
| lossy decode | 92 MB | 192 MB | 100 MB |

## Binary size and build time

Each half links only its own libwebp module (one module would keep all of libwebp, because
every function sits in the module's function table and the linker cannot drop any).
`bench/size.sh` builds a minimal program per library and operation (`CGO_ENABLED=0`,
`-trimpath`) and reports the delta over a program that uses only the standard `image` package,
plus the build time with an empty build cache. First run, against a baseline that also imported
`image/png` (2.7 MB) and with the decoder still at `-O3`:

| program | over baseline |
|---|---:|
| `webp/decode` (this package, decoder only) | +1.47 MB |
| `webp/encode` (encoder only) | +3.14 MB |
| `webp` (both) | +4.78 MB |
| gen2brain/webp, decode / encode | +3.31 MB / +4.42 MB |
| gen2brain/vpx, decode / encode | +0.28 MB / +0.63 MB |

With the decoder at `-O2` (below), the decode-only program is 0.28 MB smaller. The full table
(bare baseline, build times) is pending the benchmark window.

## Build flags

Each choice below was measured with `go test -bench` on one 384 × 256 thumbnail, `GOMAXPROCS=1`,
in ten interleaved rounds under the lock, and compared with benchstat (raw data:
`bench/results/flags/`). The tables show median time per operation and the change against the
first column (`~`: not significant at p < 0.05).

**wasm2go `-unsafe` and LTO** (`bench/results/flags/unsafe-and-lto`): `-unsafe` makes wasm2go
use unsafe loads and stores (still bounds checked). Kept both:

| | -O3 -unsafe | without -unsafe | -O3 -unsafe + LTO |
|---|---:|---:|---:|
| encode, method 0 | 2.23 ms | +9.6 % | −7.2 % |
| encode, method 4 | 12.93 ms | +17.7 % | −9.4 % |
| decode, lossy | 1.09 ms | +4.4 % | ~ |
| decode, lossless | 1.38 ms | +0.6 % | −4.5 % |

**Variants on top of LTO** (`bench/results/flags/lto-variants`): none beat plain `-O3 -flto`:

| | -O3 LTO | + -mtail-call | -O2 | -Os | -fno-unroll-loops |
|---|---:|---:|---:|---:|---:|
| encode, method 0 | 2.08 ms | ~ | +4.9 % | +25.0 % | +20.0 % |
| encode, method 4 | 11.90 ms | ~ | +5.4 % | +30.5 % | +29.9 % |
| decode, lossy | 1.06 ms | ~ | ~ | ~ | +7.2 % |
| decode, lossless | 1.32 ms | ~ | ~ | +5.1 % | ~ |

**Optimization level against size** (`bench/results/flags/opt-levels`; binary = a minimal
program using only that half, everything else equal):

| | -O3 | -O2 | -Os | -Oz |
|---|---:|---:|---:|---:|
| encode, method 0 | 2.08 ms | +5.3 % | +25.9 % | +45.7 % |
| encode, method 4 | 11.82 ms | +6.9 % | +31.4 % | +38.1 % |
| decode, lossy | 1.09 ms | ~ | +2.2 % | +37.0 % |
| decode, lossless | 1.32 ms | ~ | +5.3 % | +32.3 % |
| encoder: generated Go / binary | 2.40 MB / 5.83 MB | 1.83 MB / 5.14 MB | 1.06 MB / 4.10 MB | 0.92 MB / 3.98 MB |
| decoder: generated Go / binary | 1.24 MB / 4.16 MB | 0.98 MB / 3.88 MB | 0.68 MB / 3.53 MB | 0.50 MB / 3.37 MB |

Kept: **encoder `-O3`** (speed first: `-O2` would save 0.7 MB for 5–7 %), **decoder `-O2`** (as
fast as `-O3` and 0.28 MB smaller; `-Os` would save 0.35 MB more for 2–5 %).

## Optimization log

What was tried, in order, and kept only when it paid:

1. **Pooled instances** (prototype): one libwebp module per call allocated 5.6 MB per encode;
   reusing modules brought it to 47 KB.
2. **Output streamed to the `io.Writer`** from the module's memory (a host import) instead of
   libwebp's memory writer, which grew a buffer by reallocation and was copied out at the end.
   A steady-state encode allocates **0 B** on the Go heap.
3. **wasm2go `-unsafe`**: 8–15 % faster encodes. Kept.
4. **LTO** (`-flto`, `--lto-O3`): 7–9 % faster encodes, 0–5 % faster decodes, +1.2 MB binary.
   Kept. It pulls in wasi-libc's stack-protector seeding (`random_get`), answered with a fixed
   value.
5. **`-mtail-call`, `-fno-unroll-loops`, `-O2`, `-Os`, `-Oz`** for the encoder: all slower.
   Not kept.
6. **Separate decoder and encoder modules and packages**: a decode-only program links 1.5 MB
   instead of 4.8 MB. Kept. The decoder then moved to `-O2` (same speed, −0.28 MB).
7. **LIFO pool with idle release** (the soak above): flat heap instead of unbounded growth
   across instances. Kept.
8. **Memory reserved once per large encode**: the module's memory used to grow step by step,
   and each step reallocated and copied all of it, leaving the old copy as garbage until the
   next GC. Peak RSS for a 24 MP lossless encode fell from 3.07 GB to 0.76 GB (C: 0.71 GB), and
   for a lossy one from 237 MB to 152 MB. Kept.
9. **Strip import**: the pixels are no longer copied whole into the module. Lossy pixels stream
   through a 32-row buffer into libwebp's own RGBA import, which is exact because libwebp
   converts two rows at a time. Lossless pixels go straight into libwebp's ARGB picture. The
   output is still `cwebp`'s, byte for byte (tested at strip boundaries, odd heights and partial
   alpha). Its time and RSS effect: *pending the benchmark window*.
10. **Zero-copy large decodes**: an image of 8 MB or more, decoded on a module that holds little
    else, keeps the module's memory as its pixels. The module is discarded, as it would be
    anyway once it outgrows the pool. 24 MP decode peak: 192 MB → 100 MB (C: 92 MB). Kept.
