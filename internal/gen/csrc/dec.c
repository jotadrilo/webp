// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).
//
// dec.c: the entry points the Go package calls, compiled together with the official libwebp
// decoder sources (third_party/libwebp, BSD-3-Clause, Copyright (c) 2010, Google Inc.) to wasm32-wasi
// and translated to Go by wasm2go. It only calls libwebp's public API.
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

#include "webp/decode.h"

// wp_features reads the bitstream features of data into out[0..4]: width, height, has_alpha,
// has_animation, format (0 mixed, 1 lossy, 2 lossless). Returns a VP8StatusCode.
int wp_features(const uint8_t* data, size_t size, int* out) {
  WebPBitstreamFeatures f;
  VP8StatusCode st = WebPGetFeatures(data, size, &f);
  if (st == VP8_STATUS_OK) {
    out[0] = f.width;
    out[1] = f.height;
    out[2] = f.has_alpha;
    out[3] = f.has_animation;
    out[4] = f.format;
  }
  return st;
}

// wp_decode decodes data to non-premultiplied RGBA with libwebp's default options (those of
// dwebp), into out (stride bytes per row, size bytes). Returns a VP8StatusCode.
int wp_decode(const uint8_t* data, size_t size, uint8_t* out, size_t out_size, int stride) {
  WebPDecoderConfig config;
  if (!WebPInitDecoderConfig(&config)) return VP8_STATUS_INVALID_PARAM;
  config.output.colorspace = MODE_RGBA;
  config.output.is_external_memory = 1;
  config.output.u.RGBA.rgba = out;
  config.output.u.RGBA.stride = stride;
  config.output.u.RGBA.size = out_size;
  VP8StatusCode st = WebPDecode(data, size, &config);
  WebPFreeDecBuffer(&config.output);
  return st;
}
