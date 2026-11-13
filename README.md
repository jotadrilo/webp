# webp

[![CI](https://github.com/jotadrilo/webp/actions/workflows/ci.yml/badge.svg)](https://github.com/jotadrilo/webp/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jotadrilo/webp.svg)](https://pkg.go.dev/github.com/jotadrilo/webp)

WebP encoding and decoding in pure Go, built from Google's
[libwebp](https://chromium.googlesource.com/webm/libwebp) source (unmodified) compiled to Go; the
libwebp release it is built from is pinned in [`internal/gen/toolchain.env`](internal/gen/toolchain.env).
This is an independent project: it is not made, endorsed or supported by Google or the WebM
project.

- **Pure Go, static.** It builds with `CGO_ENABLED=0` into a static binary. No cgo, no shared
  library, no `dlopen`/purego, nothing loaded at run time.
- **Upstream libwebp, verifiable.** The Go code is the unmodified upstream libwebp compiled to WebAssembly with wasi-sdk
  and translated to Go with [wasm2go](https://github.com/ncruces/wasm2go). The libwebp source,
  the compiler and the translator are pinned, and CI regenerates the code and fails if a single
  byte differs from what is committed.
- **libwebp's exact output.** `Encode` writes the same bytes as libwebp's `cwebp`. `Decode`
  gives the same pixels as libwebp's `dwebp` (and browsers built on libwebp), lossy included.
  `golang.org/x/image/webp` decodes lossy WebP with shifted colors (see
  [BENCHMARKS.md](BENCHMARKS.md#decode)).
- **Low memory, concurrent.** Each call borrows a libwebp instance from a small pool. A
  steady-state encode of a thumbnail allocates nothing on the Go heap, a decode allocates only
  the image, and idle instances are released.

What it is not: a port. Nobody rewrote libwebp in Go. It is libwebp itself, so it is exactly as
correct as libwebp, and about 2× slower than native C libwebp. It is 1.2–1.5× faster than
[gen2brain/webp](https://github.com/gen2brain/webp), which takes the same approach, without its
per-call allocations, and slower than [gen2brain/vpx](https://github.com/gen2brain/vpx), a hand
port of libwebp to Go with assembly. See [Benchmarks](#benchmarks).

## Install

```sh
go get github.com/jotadrilo/webp
```

Requires Go 1.25 or later.

## Usage

```go
import "github.com/jotadrilo/webp"

// Encode: nil options are libwebp's defaults (lossy, quality 75, method 4).
err := webp.Encode(w, img, nil)

// Thumbnails often want method 0: about 3x faster than 4, for files ~20 % larger.
err = webp.Encode(w, img, &webp.Options{Quality: 80, Method: 0})

// Lossless, keeping the RGB under fully transparent pixels.
err = webp.Encode(w, img, &webp.Options{Lossless: true, Quality: 75, Method: 4, Exact: true})

// Decode: *image.NRGBA with alpha, *image.RGBA without.
m, err := webp.Decode(r)
cfg, err := webp.DecodeConfig(r)

// Importing the package registers "webp" with image.Decode.
m, format, err := image.Decode(r)
```

A non-nil `Options` is used as given (`Quality` 0 means quality 0), so start from
`webp.DefaultOptions()` to change a single field. Alpha is kept exactly, in lossy files too.
`*image.NRGBA`, `*image.RGBA` and `*image.Paletted` are read directly; other image types are
converted exactly like `color.NRGBAModel`.

Errors are `*webp.Error` values that wrap one of `ErrFormat`, `ErrTruncated`, `ErrUnsupported`
(animated WebP), `ErrInvalidImage`, `ErrInvalidOptions`, `ErrOutOfMemory` or `ErrInternal`; test
them with `errors.Is`. Bad input never panics (`Decode` is fuzzed). Like `image/png`, `Decode`
allocates what the header asks for, up to 16383 × 16383 × 4 bytes, so check `DecodeConfig`
first when untrusted input size matters to you.

**Only one half?** `github.com/jotadrilo/webp/decode` (`decode.Decode`, `decode.DecodeConfig`,
the `image` registration) links only the decoder, and `github.com/jotadrilo/webp/encode` only
the encoder. Each adds about half the binary size of the whole package (see
[BENCHMARKS.md](BENCHMARKS.md#binary-size-and-build-time)).

**Concurrency and memory.** `Encode` and `Decode` are safe for concurrent use. Peak memory grows
with the number of calls running at once (one libwebp instance each), so bound your concurrency
as you would for any CPU-bound work. At most `GOMAXPROCS` idle instances are kept. An instance
whose memory grew beyond 32 MB (a large image) is not kept, and idle instances are released
after 10 seconds. Large decodes take their pixels straight from libwebp's memory without a copy.

If `golang.org/x/image/webp` (or another WebP package) is also imported, `image.Decode` uses
whichever registered first. Call `webp.Decode` directly to be sure you get libwebp's colors.

## Verification

Everything that goes into `internal/gen/*/libwebp.go` is pinned in
[`internal/gen/toolchain.env`](internal/gen/toolchain.env) and
[`internal/gen/flags.env`](internal/gen/flags.env):

- the official libwebp release tarball, by URL and SHA-256 (`scripts/fetch-libwebp.sh` checks it
  and unpacks it into `third_party/libwebp/_src`; only its license files are committed),
- wasi-sdk 34, by SHA-256 for each host platform,
- wasm2go v0.4.21 (checked by the Go checksum database),
- the compiler and translator flags, and the two small C entry points in
  [`internal/gen/csrc`](internal/gen/csrc), which only call libwebp's public API.

```sh
make generate          # rebuild internal/gen from those inputs
make verify-generate   # fail if the result differs from what is committed
```

CI runs `make verify-generate` on Linux amd64 and arm64. The output is byte-identical there and
on macOS arm64. The tests then check the behavior against libwebp's own tools, built natively
from the same source (`make dwebp`):

- `Encode` output equals `cwebp`'s, byte for byte (lossy, lossless, alpha, odd sizes),
- `Decode` output equals `dwebp`'s, pixel for pixel, and `dwebp` decodes what `Encode` writes,
- lossless round trips are exact (with `Exact`, even under transparent pixels).

A weekly workflow proposes each new libwebp, wasi-sdk or wasm2go release as a pull request (see
[Updating libwebp](#updating-libwebp)). Updating is a reviewed change, never an automatic merge.

## Updating libwebp

1. **Pull request from the weekly job.** Every Monday,
   [`upstream-update.yml`](.github/workflows/upstream-update.yml) checks libwebp, wasi-sdk and
   wasm2go, each on its own. For a new release it runs `scripts/update-toolchain.sh` (the new
   version and the SHA-256 computed from the downloaded release, in `internal/gen/toolchain.env`),
   `make generate`, `make testdata` for libwebp, and the tests, including the `cwebp` byte parity
   and `dwebp` pixel parity. Then it opens a pull request "Update libwebp to X.Y.Z" from the
   branch `update/libwebp` (reused, never a duplicate), with the release notes link, and starts CI
   on it. If generation or the tests fail, it opens or updates an issue with the log instead. It
   can also be started by hand (Actions → Upstream update → Run workflow).
2. **Review.** Read the release notes, look over the diff of `internal/gen` and `testdata`, and
   optionally run `make bench` and update [BENCHMARKS.md](BENCHMARKS.md).
3. **Merge.** Nothing is merged automatically.
4. **Tag a new minor version** (for example `git tag v0.2.0 && git push origin v0.2.0`).
   [`release.yml`](.github/workflows/release.yml) then makes the GitHub release, with notes
   listing the commits since the previous tag.

To do the same by hand: `scripts/update-toolchain.sh libwebp [version]`, `make generate testdata
test`.

## Benchmarks

`make bench` (or `./bench/run.sh`) measures this package against native C libwebp,
gen2brain/webp, gen2brain/vpx, golang.org/x/image/webp and eringen/gowebper, in separate
processes, interleaved in rounds under a machine-wide lock. It reports time at 1, 2 and 4 CPUs,
throughput, allocations, GC pauses, peak RSS, output size and quality (SSIM/PSNR scored through
`dwebp`), and agreement with `dwebp`. The published numbers come from one environment, native
macOS on one Mac, recorded with each run so it can be repeated. Results, method and the
optimization log are in [BENCHMARKS.md](BENCHMARKS.md).

Final results (Apple M5 Pro, macOS 26.3, Go 1.27.4, all 18 CPUs, library defaults; 384 px
thumbnails; time per image, and as a multiple of native C libwebp measured in the same run):

| | C libwebp | **this package** | gen2brain/webp | gen2brain/vpx | x/image/webp |
|---|---:|---:|---:|---:|---:|
| encode, lossy method 0 | 1.61 ms | **3.06 ms (1.90×)** | 4.43 ms (2.75×) | 2.09 ms (1.30×) | – |
| encode, lossy method 4 | 6.50 ms | **15.5 ms (2.38×)** | 22.1 ms (3.40×) | 8.56 ms (1.32×) | – |
| encode, lossless | 33.4 ms | **54.2 ms (1.62×)** | 70.0 ms (2.10×) | 22.0 ms (0.66×) | – |
| decode | 0.51 ms | **1.04 ms (2.03×)** | 1.27 ms (2.47×) | 0.85 ms (1.65×) | 1.36 ms (2.65×) |
| Go allocation per lossy encode | – | **0 B** | 6.6 MB | 44 KB | – |
| decoded colors equal to libwebp's dwebp | yes | **yes** | no | no | no |
| binary size over a bare program (decode / encode) | – | **+2.0 / +4.1 MB** | +4.2 / +5.3 MB | +1.1 / +1.4 MB | +0.5 MB / – |

See [BENCHMARKS.md](BENCHMARKS.md) for throughput, latency percentiles, peak RSS (including a
24 MP image), quality, the 50,000-image soak, binary sizes and build times, the exact
environment, and the optimization log.

## Development

```sh
make test              # builds dwebp/cwebp from the pinned source, then go test ./... (with a 2000-image soak)
make race              # go test -race
make lint              # go vet + golangci-lint
make fuzz              # FUZZTIME=30s by default
make testdata          # regenerate testdata/ with cwebp and dwebp
```

## Licenses

This package is BSD-3-Clause ([LICENSE](LICENSE)). It contains libwebp, also BSD-3-Clause,
Copyright (c) 2010 Google Inc. ([third_party/libwebp/LICENSE](third_party/libwebp/LICENSE)),
with Google's patent grant ([third_party/libwebp/PATENTS](third_party/libwebp/PATENTS)). See
[NOTICE](NOTICE). The generated Go is a translation of libwebp and stays under libwebp's
license. The test images are synthetic, generated by `internal/testimg`.

## Support

This is published as is: there is no promise of support, fixes or response times. Issues and
pull requests are welcome, and may be answered slowly or not at all.
