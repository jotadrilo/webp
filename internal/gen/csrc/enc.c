// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).
//
// enc.c: the entry points the Go package calls, compiled together with the official libwebp
// encoder sources (third_party/libwebp, BSD-3-Clause, Copyright (c) 2010, Google Inc.) to wasm32-wasi
// and translated to Go by wasm2go. It only calls libwebp's public API.
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

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
