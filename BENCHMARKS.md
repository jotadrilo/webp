# Benchmarks

**Final** for the reference environment below, measured on 2026-11-13. To compare a change,
rerun `./bench/run.sh` on a matching machine and diff `bench/results/macos-arm64/`.

## Summary

Mean time per 384 px thumbnail (single-image latency) and throughput with 18 concurrent jobs, as
multiples of native C libwebp measured in the same run (lower is better).

| | C libwebp | **this package** | gen2brain/webp | gen2brain/vpx | x/image/webp | gowebper |
|---|---:|---:|---:|---:|---:|---:|
| encode, lossy method 0 | 1.61 ms | **1.90×** | 2.75× | 1.30× | – | – |
| encode, lossy method 4 | 6.50 ms | **2.38×** | 3.40× | 1.32× | – | – |
| encode, lossless | 33.4 ms | **1.62×** | 2.10× | 0.66× | – | 4.51× |
| decode | 0.51 ms | **2.03×** | 2.47× | 1.65× | 2.65× | – |
| encode throughput, method 0 | 8995 img/s | **2.24×** | 3.96× | 1.45× | – | – |
| decode throughput | 27870 img/s | **2.56×** | 4.19× | 1.92× | 3.56× | – |
| Go allocation per lossy encode | – | **0 B** | 6.6–8.9 MB | 44–157 KB | – | – |
| same bytes as cwebp / pixels as dwebp | yes | **yes / yes** | yes / no¹ | no / no² | – / no² | no³ / – |

¹ gen2brain/webp v0.6.4 returns every decoded image, lossless included, as `*image.NYCbCrA`,
so lossless pixels are not preserved and lossy colors go through Go's full-range YCbCr
conversion: PSNR 28.4 dB against dwebp on opaque lossy images, and a largest difference of 240.
² Lossy images come back as YCbCr planes, with the same color shift (28.4 dB); lossless is exact.
gen2brain/vpx is a port with its own encoder: valid output, but not libwebp's bytes (2–14 %
larger here, at the same or slightly higher quality).
³ eringen/gowebper writes lossless only. Its output is not lossless (51.7 dB), and libwebp's dwebp
rejects 2 of its 7 files.

This package is libwebp itself, so its output and quality are exactly libwebp's. It costs about
2× native C's time, is 1.2–1.5× faster than gen2brain/webp (the same approach) with no
per-encode allocation, and is slower than gen2brain/vpx (a hand port with assembly, and the only
implementation here that uses several cores for one image).

## Environment and method

```
date:        2026-11-13T01:51Z
command:     ./run.sh macos-arm64  (ROUNDS=10 PHASES="quality rounds big soak size report" SOAK=50000 LOADMAX=9)
priority:    nice 0 (nice -5 needs root), no QoS clamp, taskpolicy -t 0 -l 0
model:       Mac17,9
chip:        Apple M5 Pro; 6 Super + 12 Performance cores, 48 GB
macOS:       26.3.2 (25D2150)
power:       AC Power; 100%; charged; 0:00 remaining present: true
power mode:  powermode=0 
load before: 2.44 3.34 3.97
go:          go version go1.27.4 darwin/arm64
cc:          Apple clang version 21.0.0 (clang-2100.1.1.101)
this repo:   703828b
libwebp:     1.6.0 (C baseline, dwebp/cwebp)
Go packages: github.com/gen2brain/webp@v0.6.4 github.com/gen2brain/vpx@v0.2.2 golang.org/x/image@v0.47.0 github.com/eringen/gowebper@v0.1.2 
config:      GOMAXPROCS unset (18 CPUs), library defaults, throughput with 18 jobs; 10 interleaved rounds; load gate < 9
```

The 1-minute load during the ten rounds: min 2.41, median 5.58, max 8.88. Other work (builds,
editors) ran on the machine, but no other benchmark did: every timed process held the
machine-wide lock. The soak, binary-size and report phases ran afterwards at commit `0508ea6`
(only the soak test's check changed). Command:

```sh
./bench/run.sh macos-arm64     # = make bench; ROUNDS=10 SOAK=50000 by default
```

- **One configuration**, the machine as it is: native macOS, `GOMAXPROCS` unset (all 18 CPUs),
  every library at its defaults. C libwebp uses no threads within one image (its default, like
  `cwebp`/`dwebp` without `-mt`). gen2brain/vpx codes a lossy image over GOMAXPROCS goroutines by
  default, so it is the only implementation that uses several cores per image. Throughput runs
  as many concurrent jobs as CPUs (18).
- **Implementations:** native C libwebp 1.6.0 (`bench/c/cbench.c`, built from the same pinned
  source with its NEON code, `-O2`), this package, gen2brain/webp v0.6.4 (`-tags nodynamic`: its
  WASM, never a system libwebp), gen2brain/vpx v0.2.2, golang.org/x/image/webp v0.47.0 (decoder
  only), eringen/gowebper v0.1.2 (lossless encoder only).
- **Settings:** lossy quality 80 at method 0 (thumbnails) and 4 (libwebp's default), lossless at
  the default effort.
- **Inputs:** seven synthetic 384 px thumbnails from `internal/testimg` (photo-like, flat, line
  art, gradient, noise, and two with alpha) and a 24 MP photo-like image. Decoders get the
  thumbnails encoded by libwebp's `cwebp`, lossy (q80 m4) and lossless.
- **Statistics:** each implementation runs in its own process (its allocations, GC and RSS are
  its own), interleaved in ten rounds (C, A, B, …, C, A, B, …). Each process holds the machine
  lock `/tmp/jotadrilo-bench.lock` and starts only when the 1-minute load is below 9. Priority is
  the default with no QoS clamp, plus `taskpolicy -t 0 -l 0` (`nice -5` needs root). One Go
  toolchain builds everything. Tables give the median over rounds of each image's mean time;
  `benchstat` gives confidence intervals and significance with C as the base column
  (`bench/results/macos-arm64/benchstat-*.txt`).
- **Quality:** output decoded by libwebp's `dwebp`; SSIM on luma and PSNR on RGB, both composited
  over white; alpha compared exactly. Decoders are compared with `dwebp`'s pixels in the colors a
  viewer sees (premultiplied, so RGB under fully transparent pixels does not count).

## Results

#### Encode: Lossy, quality 80, method 0 (thumbnail setting)

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp-m0 | 1.61 | 1.00× | 1.60 / 1.66 / 1.74 | – | – | 112.3 | 0.9661 | 36.49 | yes | 0 |
| vpx-m0 | 2.09 | 1.30× | 2.08 / 2.14 / 2.24 | 44 | 0 | 117.8 | 0.9663 | 36.49 | yes | 0 |
| ours-m0 | 3.06 | 1.90× | 3.05 / 3.17 / 3.22 | 0 | 0 | 112.3 | 0.9661 | 36.49 | yes | 0 |
| gen2brain-m0 | 4.43 | 2.75× | 4.42 / 4.61 / 4.71 | 6587 | 26 | 112.3 | 0.9661 | 36.49 | yes | 0 |

#### Encode: Lossy, quality 80, method 4 (libwebp's default method)

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp-m4 | 6.50 | 1.00× | 6.48 / 6.63 / 6.68 | – | – | 102.9 | 0.9665 | 36.48 | yes | 0 |
| vpx-m4 | 8.56 | 1.32× | 8.54 / 8.69 / 8.77 | 157 | 0 | 117.6 | 0.9683 | 36.65 | yes | 0 |
| ours-m4 | 15.5 | 2.38× | 15.4 / 15.6 / 15.7 | 0 | 0 | 102.9 | 0.9665 | 36.48 | yes | 0 |
| gen2brain-m4 | 22.1 | 3.40× | 22.1 / 22.4 / 22.5 | 8874 | 34 | 102.9 | 0.9665 | 36.48 | yes | 0 |

#### Encode: Lossless, default effort

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| vpx-lossless | 22.0 | 0.66× | 21.9 / 22.5 / 22.5 | 1096 | 2 | 529.4 | 1.0000 | 99.00 | yes | 0 |
| c-libwebp-lossless | 33.4 | 1.00× | 33.4 / 33.5 / 33.6 | – | – | 517.0 | 1.0000 | 99.00 | yes | 0 |
| ours-lossless | 54.2 | 1.62× | 54.0 / 54.9 / 55.0 | 12664 | 8 | 517.0 | 1.0000 | 99.00 | yes | 0 |
| gen2brain-lossless | 70.0 | 2.10× | 69.8 / 71.0 / 71.0 | 52003 | 45 | 517.0 | 1.0000 | 99.00 | yes | 0 |
| gowebper-lossless | 151 | 4.51× | 151 / 151 / 151 | 7235 | 30 | 669.5 | 0.9829 | 51.65 | yes | 2 |

#### Decode

The 14 inputs are the thumbnails encoded by libwebp's cwebp, lossy (q80 m4) and lossless. Agreement with dwebp, in the colors a viewer sees (premultiplied, so the RGB under fully transparent pixels does not count): the largest per-channel difference from libwebp's own decoding over all inputs, and the mean PSNR against it on the opaque lossy inputs and on the lossy inputs with alpha (99: identical).

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | max diff vs dwebp | PSNR vs dwebp, opaque lossy | PSNR vs dwebp, lossy with alpha | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp | 0.51 | 1.00× | 0.51 / 0.54 / 0.56 | – | – | 0 (it is dwebp's decoder) | 99 | 99 | 0 |
| vpx | 0.85 | 1.65× | 0.84 / 0.92 / 0.95 | 391 | 5 | 89 | 28.40 | 34.49 | 0 |
| ours | 1.04 | 2.03× | 1.03 / 1.11 / 1.16 | 399 | 4 | 0 | 99.00 | 99.00 | 0 |
| gen2brain | 1.27 | 2.47× | 1.26 / 1.35 / 1.40 | 1751 | 18 | 240 | 28.40 | 34.49 | 0 |
| ximage | 1.36 | 2.65× | 1.34 / 1.46 / 1.52 | 415 | 5 | 89 | 28.40 | 34.49 | 0 |

#### Encode: throughput with as many concurrent jobs as CPUs

Images per second over the thumbnails, one job per CPU; × C is native libwebp's throughput over this one's.

| implementation | img/s (18 jobs) | × C | peak RSS MB |
|---|---:|---:|---:|
| c-libwebp-m0 | 8995 | 1.00× | 55 |
| vpx-m0 | 6196 | 1.45× | 359 |
| ours-m0 | 4018 | 2.24× | 217 |
| gen2brain-m0 | 2270 | 3.96× | 314 |
| c-libwebp-m4 | 2175 | 1.00× | 91 |
| vpx-m4 | 1591 | 1.37× | 422 |
| ours-m4 | 756 | 2.88× | 251 |
| vpx-lossless | 602 | 0.67× | 449 |
| gen2brain-m4 | 484 | 4.50× | 318 |
| c-libwebp-lossless | 403 | 1.00× | 661 |
| ours-lossless | 222 | 1.81× | 1680 |
| gen2brain-lossless | 150 | 2.68× | 1240 |
| gowebper-lossless | 28 | 14.57× | 171 |

#### Decode: throughput with as many concurrent jobs as CPUs

Images per second over the thumbnails, one job per CPU; × C is native libwebp's throughput over this one's.

| implementation | img/s (18 jobs) | × C | peak RSS MB |
|---|---:|---:|---:|
| c-libwebp | 27870 | 1.00× | 36 |
| vpx | 14504 | 1.92× | 128 |
| ours | 10869 | 2.56× | 109 |
| ximage | 7834 | 3.56× | 62 |
| gen2brain | 6645 | 4.19× | 124 |

#### One 24 MP image (6000 × 4000)

Time of one encode or decode after a warm-up, and the peak RSS of the process above what it held before (the 96 MB RGBA source of an encode is in the baseline).

| operation | implementation | time ms | peak RSS above baseline MB |
|---|---|---:|---:|
| Encode | c-libwebp-m0 | 195 | 39 |
| Encode | vpx-m0 | 305 | 739 |
| Encode | ours-m0 | 456 | 134 |
| Encode | gen2brain-m0 | 658 | 598 |
| Encode | c-libwebp-lossless | 8637 | 711 |
| Encode | vpx-lossless | 8773 | 2424 |
| Encode | ours-lossless | 12070 | 760 |
| Encode | gen2brain-lossless | 15715 | 2789 |
| Decode | vpx | 69.3 | 145 |
| Decode | c-libwebp | 69.4 | 92 |
| Decode | gen2brain | 212 | 138 |
| Decode | ximage | 222 | 37 |
| Decode | ours | 249 | 102 |

## Memory

### Soak: 50,000 images

`TestSoak` encodes and decodes 50,000 mixed images (1 px to 640 px, every kind, with and without
alpha, lossy and lossless) with a burst of 4 × GOMAXPROCS concurrent goroutines every 5,000
images. Every 1,000 images it collects garbage and records the heap. Then it releases every idle
pooled instance, collects again, returns freed memory to the OS, and records the drained heap and
the RSS. The drained heap is what would remain if nothing leaked. It must stay flat, and does:

| images | 1,000 | 5,000 | 10,000 | 20,000 | 30,000 | 40,000 | 50,000 |
|---|---:|---:|---:|---:|---:|---:|---:|
| heap in use, pools as they are, MB | 24.6 | 104.3 | 77.1 | 93.1 | 88.9 | 81.5 | 86.6 |
| heap in use, pools drained, MB | 15.17 | 15.41 | 15.43 | 15.51 | 15.59 | 15.65 | 15.67 |
| heap objects, pools drained | 0.6k | 0.9k | 1.0k | 1.2k | 1.4k | 1.5k | 1.6k |
| RSS, MB | 67 | 366 | 366 | 367 | 367 | 368 | 402 |

(The pools keep up to 18 encoder and 18 decoder instances after a burst, sized by the images
they last served; the series is `bench/results/macos-arm64/soak.csv`.) The heap-profile diff
between image 1,000 and image 10,000 (`soak-heap-diff.txt`) holds 2.5 MB, all in the Go runtime
(OS threads and timers for the bursts), and nothing from this package. CI runs a 2,000-image
soak on every push, and `goleak` checks that no goroutine is left.

The soak found two problems, both fixed. First, the pool was a FIFO, so sequential calls
rotated through every idle instance and grew all of them: the heap went from 25 MB to 184 MB in
2,000 images. It is now a stack. Second, an up-front memory reserve for small lossless encodes
kept 300 MB pooled after a burst. The reserve now applies only above the pool's 32 MB bound.

### One 24 MP image

Above, in the last results table: this package's peak RSS above baseline is 134 MB for a lossy
encode (C: 39 MB; gen2brain/webp: 598 MB; gen2brain/vpx: 739 MB), 760 MB for a lossless one
(C: 711 MB; the others 2.4–2.8 GB) and 102 MB for a decode (C: 92 MB). Its 24 MP decode is
slower than gen2brain/webp's and x/image's because they return YCbCr planes, skipping libwebp's
RGB conversion (and getting the colors wrong).

### The last memory changes, before and after

The same harness, built against the library before (`00e8228`) and after the memory reserve,
the zero-copy large decode and the strip import, interleaved, ten rounds
(`bench/results/macos-arm64/before-after/`):

| | before | after | change |
|---|---:|---:|---:|
| thumbnail time, method 0 / method 4 / lossless / decode | | | +0.25 % / +0.34 % / −0.33 % / +0.19 %, none significant |
| throughput, method 0 / method 4 / lossless | 4142 / 794 / 226 img/s | 4124 / 784 / 227 img/s | not significant |
| throughput peak RSS, method 0 / method 4 / lossless | 207 / 258 / 1568 MiB | 206 / 241 / 1565 MiB | ~ / −6 % / ~ |
| 24 MP lossy encode, peak RSS above baseline | 238 MB | 134 MB | −44 % |
| 24 MP lossless encode | 2268 MB | 760 MB | −66 % |
| 24 MP decode | 193 MB | 102 MB | −47 % |

## Binary size and build time

A minimal program per library and operation (`bench/size.sh`: `CGO_ENABLED=0`, `-trimpath`),
against a baseline that uses only the standard `image` package. Build time is the faster of two
builds, each with an empty build cache:

| program | binary | over baseline | clean build s | over baseline |
|---|---:|---:|---:|---:|
| baseline (image only) | 1.8 MB | – | 1.0 | – |
| ours-decode | 3.8 MB | +2.01 MB | 1.7 | +0.7 |
| ours-encode | 5.9 MB | +4.06 MB | 2.7 | +1.7 |
| ours-both | 7.3 MB | +5.43 MB | 2.8 | +1.8 |
| gen2brain-decode | 6.0 MB | +4.15 MB | 2.9 | +1.9 |
| gen2brain-encode | 7.1 MB | +5.28 MB | 2.9 | +1.9 |
| vpx-decode | 2.9 MB | +1.06 MB | 1.4 | +0.4 |
| vpx-encode | 3.2 MB | +1.41 MB | 1.4 | +0.4 |
| ximage-decode | 2.4 MB | +0.53 MB | 1.2 | +0.2 |
| gowebper-encode | 2.0 MB | +0.18 MB | 1.2 | +0.2 |

`webp/decode` and `webp/encode` link only their half of libwebp. One module for both would keep
all of it, because every function sits in the module's function table and the linker can drop
none.

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
   next GC. 24 MP lossless: 2.27 GB → 0.76 GB peak above baseline (C: 0.71 GB). Kept, only
   above the pool's 32 MB bound: below it, the soak showed that reserving only inflated pooled
   instances.
9. **Strip import**: the pixels are no longer copied whole into the module. Lossy pixels stream
   through a 32-row buffer into libwebp's own RGBA import, which is exact because libwebp
   converts two rows at a time. Lossless pixels go straight into libwebp's ARGB picture. The
   output is still `cwebp`'s, byte for byte (tested at strip boundaries, odd heights and partial
   alpha). With 8: 24 MP lossy encode 238 MB → 134 MB above baseline (C: 39 MB, which reads the
   caller's pixels directly), and no measurable time change.
10. **Zero-copy large decodes**: an image of 8 MB or more, decoded on a module that holds little
    else, keeps the module's memory as its pixels. The module is discarded, as it would be
    anyway once it outgrows the pool. 24 MP decode peak: 193 MB → 102 MB (C: 92 MB), same time.
    Kept.
11. **The benchmark's verdict on what is left:** a thumbnail costs about 2× native C (lossless
    1.6×). The remaining gap is wasm2go's translation of libwebp's scalar C: no SIMD, indirect
    calls through a function table, and bounds-checked memory. None of these is a knob left to
    turn here. Faster would need a port, as gen2brain/vpx does.
