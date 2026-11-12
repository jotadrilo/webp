// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).
//
// enc.c: the entry points the Go package calls, compiled together with the official libwebp
// encoder sources (third_party/libwebp, BSD-3-Clause, Copyright (c) 2010, Google Inc.) to wasm32-wasi
// and translated to Go by wasm2go. It only calls libwebp's public API.
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

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

// A picture being filled by the Go side, strip by strip, before wp_encode.
typedef struct {
  WebPPicture pic;
  int any_alpha;  // a strip had a pixel with alpha < 255
} wp_picture_t;

// wp_picture allocates a width x height picture: ARGB for lossless (the Go side writes it
// directly through wp_argb), YUV420 + alpha for lossy (filled with wp_import_rows). Returns
// NULL when out of memory.
wp_picture_t* wp_picture(int width, int height, int lossless) {
  wp_picture_t* p = malloc(sizeof *p);
  if (p == NULL) return NULL;
  if (!WebPPictureInit(&p->pic)) {
    free(p);
    return NULL;
  }
  p->any_alpha = 0;
  p->pic.width = width;
  p->pic.height = height;
  p->pic.use_argb = lossless;
  p->pic.colorspace = WEBP_YUV420A;
  p->pic.writer = Write;
  if (!WebPPictureAlloc(&p->pic)) {
    WebPPictureFree(&p->pic);
    free(p);
    return NULL;
  }
  return p;
}

uint32_t* wp_argb(wp_picture_t* p) { return p->pic.argb; }
int wp_argb_stride(wp_picture_t* p) { return p->pic.argb_stride; }

void wp_picture_free(wp_picture_t* p) {
  WebPPictureFree(&p->pic);
  free(p);
}

// wp_import_rows converts rows [y0, y0+rows) of a lossy picture from non-premultiplied RGBA
// (Go's image.NRGBA layout) with libwebp's own import, WebPPictureImportRGBA (RGBX when the
// strip is opaque), through a strip-sized picture, and copies its planes in place. y0 must be
// even and rows even except for the last strip: libwebp converts two rows at a time, deciding
// per row pair whether to weight chroma by alpha, so the result is the same as importing the
// whole image at once. Returns 0 or a WebPEncodingError.
int wp_import_rows(wp_picture_t* p, const uint8_t* rgba, int stride, int y0, int rows,
                   int opaque) {
  WebPPicture* const dst = &p->pic;
  WebPPicture strip;
  if (!WebPPictureInit(&strip)) return VP8_ENC_ERROR_OUT_OF_MEMORY;
  strip.width = dst->width;
  strip.height = rows;
  strip.use_argb = 0;
  int ok = opaque ? WebPPictureImportRGBX(&strip, rgba, stride)
                  : WebPPictureImportRGBA(&strip, rgba, stride);
  if (!ok) {
    int err = strip.error_code;
    WebPPictureFree(&strip);
    return err ? err : VP8_ENC_ERROR_OUT_OF_MEMORY;
  }
  const int w = dst->width, uv_w = (w + 1) >> 1;
  for (int y = 0; y < rows; ++y) {
    memcpy(dst->y + (size_t)(y0 + y) * dst->y_stride, strip.y + (size_t)y * strip.y_stride, w);
    if (strip.a != NULL) {
      memcpy(dst->a + (size_t)(y0 + y) * dst->a_stride, strip.a + (size_t)y * strip.a_stride, w);
    } else {
      memset(dst->a + (size_t)(y0 + y) * dst->a_stride, 0xff, w);
    }
  }
  for (int y = 0; y < (rows + 1) >> 1; ++y) {
    memcpy(dst->u + (size_t)(y0 / 2 + y) * dst->uv_stride, strip.u + (size_t)y * strip.uv_stride, uv_w);
    memcpy(dst->v + (size_t)(y0 / 2 + y) * dst->uv_stride, strip.v + (size_t)y * strip.uv_stride, uv_w);
  }
  if (strip.a != NULL) p->any_alpha = 1;
  WebPPictureFree(&strip);
  return VP8_ENC_OK;
}

// wp_encode encodes the filled picture, streaming the WebP file through host_write, and frees
// it. A lossy picture none of whose strips had alpha drops its alpha plane, as a whole-image
// import of an opaque image does. Returns 0 or a WebPEncodingError / WP_ERR_*.
int wp_encode(wp_picture_t* p, float quality, int method, int lossless, int exact) {
  WebPConfig config;
  WebPPicture* const pic = &p->pic;
  int err = VP8_ENC_OK;
  if (!WebPConfigInit(&config)) {
    err = WP_ERR_CONFIG;
    goto done;
  }
  config.quality = quality;
  config.method = method;
  config.lossless = lossless;
  config.exact = exact;
  if (!WebPValidateConfig(&config)) {
    err = WP_ERR_CONFIG;
    goto done;
  }
  if (!lossless && !p->any_alpha) {
    pic->colorspace = WEBP_YUV420;
    pic->a = NULL;
    pic->a_stride = 0;
  }
  if (!WebPEncode(&config, pic)) err = pic->error_code ? pic->error_code : VP8_ENC_ERROR_OUT_OF_MEMORY;
done:
  wp_picture_free(p);
  return err;
}
