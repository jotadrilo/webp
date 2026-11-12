// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).
//
// shim.c: the entry points the Go package calls, compiled together with the official libwebp
// sources (third_party/libwebp, BSD-3-Clause, Copyright (c) 2010, Google Inc.) to wasm32-wasi
// and translated to Go by wasm2go. It only calls libwebp's public API.
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

#include "webp/decode.h"
#include "webp/encode.h"

// Implemented in Go: appends encoded bytes to the caller's io.Writer. Returns 1 on success.
__attribute__((import_module("host"), import_name("write"))) int host_write(const uint8_t* data,
                                                                              size_t size);

static int Write(const uint8_t* data, size_t size, const WebPPicture* pic) {
  (void)pic;
  return size == 0 || host_write(data, size);
}

// Error codes above libwebp's WebPEncodingError range.
#define WP_ERR_CONFIG 100

// wp_encode encodes width x height non-premultiplied RGBA (Go's image.NRGBA layout) at rgba with
// the given stride, streaming the WebP file through host_write. opaque != 0 promises every alpha
// is 255 (the alpha channel is then ignored). It takes ownership of rgba (a malloc'd buffer) and
// frees it as soon as the picture has imported it, before encoding, to lower peak memory.
// Returns 0 or a WebPEncodingError / WP_ERR_*.
int wp_encode(uint8_t* rgba, int width, int height, int stride, float quality, int method,
              int lossless, int exact, int opaque) {
  WebPConfig config;
  WebPPicture pic;
  if (!WebPConfigInit(&config) || !WebPPictureInit(&pic)) {
    free(rgba);
    return WP_ERR_CONFIG;
  }
  config.quality = quality;
  config.method = method;
  config.lossless = lossless;
  config.exact = exact;
  if (!WebPValidateConfig(&config)) {
    free(rgba);
    return WP_ERR_CONFIG;
  }
  pic.width = width;
  pic.height = height;
  pic.use_argb = lossless;
  pic.writer = Write;
  int ok = opaque ? WebPPictureImportRGBX(&pic, rgba, stride)
                  : WebPPictureImportRGBA(&pic, rgba, stride);
  free(rgba);
  if (!ok) {
    int err = pic.error_code;
    WebPPictureFree(&pic);
    return err ? err : VP8_ENC_ERROR_OUT_OF_MEMORY;
  }
  ok = WebPEncode(&config, &pic);
  int err = ok ? VP8_ENC_OK : pic.error_code;
  WebPPictureFree(&pic);
  return ok || err ? err : VP8_ENC_ERROR_OUT_OF_MEMORY;
}

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
