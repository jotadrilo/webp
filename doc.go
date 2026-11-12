// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).

// Package webp encodes and decodes WebP images with Google's official libwebp, in pure Go.
//
// libwebp (third_party/libwebp, BSD-3-Clause, Copyright (c) 2010 Google Inc.) is compiled to
// WebAssembly and translated to Go source with wasm2go, so the package needs no cgo, no shared
// library and no dynamic loading: it builds with CGO_ENABLED=0 into a static binary. The
// generated code is reproducible from the pinned libwebp release (`make verify-generate`).
//
// Encode and Decode are safe for concurrent use. Each call borrows one of a small pool of
// libwebp instances, so steady-state calls allocate little beyond the decoded image.
//
// Importing the package registers the "webp" format with the image package. If another WebP
// decoder (such as golang.org/x/image/webp) is also imported, image.Decode uses whichever
// registered first; call Decode directly to be sure of libwebp's colors.
package webp
