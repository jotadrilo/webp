Environment:

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

### Encode: Lossy, quality 80, method 0 (thumbnail setting)

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp-m0 | 1.61 | 1.00× | 1.60 / 1.66 / 1.74 | – | – | 112.3 | 0.9661 | 36.49 | yes | 0 |
| vpx-m0 | 2.09 | 1.30× | 2.08 / 2.14 / 2.24 | 44 | 0 | 117.8 | 0.9663 | 36.49 | yes | 0 |
| ours-m0 | 3.06 | 1.90× | 3.05 / 3.17 / 3.22 | 0 | 0 | 112.3 | 0.9661 | 36.49 | yes | 0 |
| gen2brain-m0 | 4.43 | 2.75× | 4.42 / 4.61 / 4.71 | 6587 | 26 | 112.3 | 0.9661 | 36.49 | yes | 0 |

### Encode: Lossy, quality 80, method 4 (libwebp's default method)

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp-m4 | 6.50 | 1.00× | 6.48 / 6.63 / 6.68 | – | – | 102.9 | 0.9665 | 36.48 | yes | 0 |
| vpx-m4 | 8.56 | 1.32× | 8.54 / 8.69 / 8.77 | 157 | 0 | 117.6 | 0.9683 | 36.65 | yes | 0 |
| ours-m4 | 15.5 | 2.38× | 15.4 / 15.6 / 15.7 | 0 | 0 | 102.9 | 0.9665 | 36.48 | yes | 0 |
| gen2brain-m4 | 22.1 | 3.40× | 22.1 / 22.4 / 22.5 | 8874 | 34 | 102.9 | 0.9665 | 36.48 | yes | 0 |

### Encode: Lossless, default effort

Single-image latency: mean over the 7 thumbnails of each one's median time across rounds (ms); × C is the ratio to native libwebp measured in the same run.

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| vpx-lossless | 22.0 | 0.66× | 21.9 / 22.5 / 22.5 | 1096 | 2 | 529.4 | 1.0000 | 99.00 | yes | 0 |
| c-libwebp-lossless | 33.4 | 1.00× | 33.4 / 33.5 / 33.6 | – | – | 517.0 | 1.0000 | 99.00 | yes | 0 |
| ours-lossless | 54.2 | 1.62× | 54.0 / 54.9 / 55.0 | 12664 | 8 | 517.0 | 1.0000 | 99.00 | yes | 0 |
| gen2brain-lossless | 70.0 | 2.10× | 69.8 / 71.0 / 71.0 | 52003 | 45 | 517.0 | 1.0000 | 99.00 | yes | 0 |
| gowebper-lossless | 151 | 4.51× | 151 / 151 / 151 | 7235 | 30 | 669.5 | 0.9829 | 51.65 | yes | 2 |

### Decode

The 14 inputs are the thumbnails encoded by libwebp's cwebp, lossy (q80 m4) and lossless. Agreement with dwebp, in the colors a viewer sees (premultiplied, so the RGB under fully transparent pixels does not count): the largest per-channel difference from libwebp's own decoding over all inputs, and the mean PSNR against it on the opaque lossy inputs and on the lossy inputs with alpha (99: identical).

| implementation | ms/image (18 CPUs) | × C | p50 / p95 / p99 ms | alloc KB/op | GC pause µs/op | max diff vs dwebp | PSNR vs dwebp, opaque lossy | PSNR vs dwebp, lossy with alpha | errors |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| c-libwebp | 0.51 | 1.00× | 0.51 / 0.54 / 0.56 | – | – | 0 (it is dwebp's decoder) | 99 | 99 | 0 |
| vpx | 0.85 | 1.65× | 0.84 / 0.92 / 0.95 | 391 | 5 | 89 | 28.40 | 34.49 | 0 |
| ours | 1.04 | 2.03× | 1.03 / 1.11 / 1.16 | 399 | 4 | 0 | 99.00 | 99.00 | 0 |
| gen2brain | 1.27 | 2.47× | 1.26 / 1.35 / 1.40 | 1751 | 18 | 240 | 28.40 | 34.49 | 0 |
| ximage | 1.36 | 2.65× | 1.34 / 1.46 / 1.52 | 415 | 5 | 89 | 28.40 | 34.49 | 0 |

### Encode: throughput with as many concurrent jobs as CPUs

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

### Decode: throughput with as many concurrent jobs as CPUs

Images per second over the thumbnails, one job per CPU; × C is native libwebp's throughput over this one's.

| implementation | img/s (18 jobs) | × C | peak RSS MB |
|---|---:|---:|---:|
| c-libwebp | 27870 | 1.00× | 36 |
| vpx | 14504 | 1.92× | 128 |
| ours | 10869 | 2.56× | 109 |
| ximage | 7834 | 3.56× | 62 |
| gen2brain | 6645 | 4.19× | 124 |

### One 24 MP image (6000 × 4000)

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

