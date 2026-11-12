// Copyright (c) 2026, jotadrilo. BSD-3-Clause (see LICENSE).
//
// cbench: native libwebp, the ceiling of the benchmark. Same inputs as cmd/bench (the .rgba and
// .webp files of `bench -prep`), same settings, same output (Go benchmark lines). With more
// than one CPU it lets libwebp use threads within one image (WebPConfig.thread_level,
// WebPDecoderOptions.use_threads), as cwebp -mt and dwebp -mt do.
//
//   cbench <in> latency|throughput|big enc|dec <m0|m4|lossless|-> <cpus> [outdir]
#include <math.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <time.h>

#include "webp/decode.h"
#include "webp/encode.h"

static const char* kinds[] = {"photo", "flat", "lineart", "gradient", "cutout-alpha", "icon-alpha", "noise"};
#define NKINDS 7

typedef struct {
  char name[64];
  int w, h;
  uint8_t* px;     // encode input
  uint8_t* data;   // decode input
  size_t size;
} input_t;

static int g_dec, g_lossless, g_method, g_mt;

static double now_ns(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return t.tv_sec * 1e9 + t.tv_nsec;
}

static double rss_mb(void) {
  struct rusage ru;
  getrusage(RUSAGE_SELF, &ru);
#ifdef __APPLE__
  return ru.ru_maxrss / 1048576.0;
#else
  return ru.ru_maxrss / 1024.0;
#endif
}

static uint8_t* read_file(const char* p, size_t* n) {
  FILE* f = fopen(p, "rb");
  if (!f) { fprintf(stderr, "cbench: cannot open %s\n", p); exit(1); }
  fseek(f, 0, SEEK_END);
  *n = (size_t)ftell(f);
  fseek(f, 0, SEEK_SET);
  uint8_t* b = malloc(*n);
  if (fread(b, 1, *n, f) != *n) { fprintf(stderr, "cbench: short read %s\n", p); exit(1); }
  fclose(f);
  return b;
}

static void load(input_t* in, const char* dir, const char* name, const char* variant) {
  char p[1024];
  snprintf(in->name, sizeof in->name, "%s%s%s", name, variant ? "-" : "", variant ? variant : "");
  if (variant) {
    snprintf(p, sizeof p, "%s/%s-%s.webp", dir, name, variant);
    in->data = read_file(p, &in->size);
  } else {
    size_t n;
    snprintf(p, sizeof p, "%s/%s.rgba", dir, name);
    uint8_t* b = read_file(p, &n);
    in->w = b[0] | b[1] << 8 | b[2] << 16 | b[3] << 24;
    in->h = b[4] | b[5] << 8 | b[6] << 16 | b[7] << 24;
    in->px = b + 8;
  }
}

// run does one encode or decode of in; with keep, returns the encoded output (free with WebPFree).
static size_t run(const input_t* in, uint8_t** keep) {
  if (g_dec) {
    WebPDecoderConfig c;
    WebPInitDecoderConfig(&c);
    c.output.colorspace = MODE_RGBA;
    c.options.use_threads = g_mt;
    if (WebPDecode(in->data, in->size, &c) != VP8_STATUS_OK) { fprintf(stderr, "decode failed\n"); exit(1); }
    WebPFreeDecBuffer(&c.output);
    return 0;
  }
  WebPConfig c;
  WebPPicture pic;
  WebPMemoryWriter wr;
  WebPConfigInit(&c);
  c.quality = g_lossless ? 75 : 80;
  c.method = g_method;
  c.lossless = g_lossless;
  c.thread_level = g_mt;
  WebPPictureInit(&pic);
  pic.width = in->w;
  pic.height = in->h;
  pic.use_argb = g_lossless;
  WebPPictureImportRGBA(&pic, in->px, in->w * 4);
  WebPMemoryWriterInit(&wr);
  pic.writer = WebPMemoryWrite;
  pic.custom_ptr = &wr;
  if (!WebPEncode(&c, &pic)) { fprintf(stderr, "encode failed\n"); exit(1); }
  WebPPictureFree(&pic);
  if (keep) *keep = wr.mem; else WebPMemoryWriterClear(&wr);
  return wr.size;
}

static int cmpd(const void* a, const void* b) {
  double x = *(const double*)a, y = *(const double*)b;
  return x < y ? -1 : x > y;
}

static input_t ins[2 * NKINDS];
static int nin;
static volatile int stop;
static long counts[64];

static void* worker(void* arg) {
  long w = (long)arg;
  for (long k = w; !stop; k++) {
    run(&ins[k % nin], NULL);
    counts[w]++;
  }
  return NULL;
}

int main(int argc, char** argv) {
  if (argc < 6) { fprintf(stderr, "usage: cbench <in> latency|throughput|big enc|dec <setting> <cpus> [outdir]\n"); return 2; }
  const char *dir = argv[1], *mode = argv[2], *setting = argv[4], *outdir = argc > 6 ? argv[6] : NULL;
  int cpus = atoi(argv[5]);
  g_dec = !strcmp(argv[3], "dec");
  g_lossless = !strcmp(setting, "lossless");
  g_method = !strcmp(setting, "m0") ? 0 : 4;
  g_mt = cpus > 1;
  char impl[64];
  if (g_dec) snprintf(impl, sizeof impl, "c-libwebp");
  else snprintf(impl, sizeof impl, "c-libwebp-%s", setting);
  const char* op = g_dec ? "Decode" : "Encode";

  if (!strcmp(mode, "big")) {
    input_t in = {0};
    load(&in, dir, "photo-24mp", g_dec ? "lossy" : NULL);
    double base = rss_mb();
    run(&in, NULL);
    double t = now_ns();
    run(&in, NULL);
    printf("Benchmark%s24MP/impl=%s/cpus=%d\t1\t%.0f ns/op\t%.1f peak-RSS-MB\t%.1f base-RSS-MB\n", op, impl, cpus,
           now_ns() - t, rss_mb(), base);
    return 0;
  }
  for (int k = 0; k < NKINDS; k++) {
    if (g_dec) {
      load(&ins[nin++], dir, kinds[k], "lossy");
      load(&ins[nin++], dir, kinds[k], "lossless");
    } else {
      load(&ins[nin++], dir, kinds[k], NULL);
    }
  }
  if (outdir && !g_dec) {  // outputs for scoring
    for (int i = 0; i < nin; i++) {
      uint8_t* o;
      size_t n = run(&ins[i], &o);
      char p[1024];
      snprintf(p, sizeof p, "%s/%s.webp", outdir, ins[i].name);
      FILE* f = fopen(p, "wb");
      fwrite(o, 1, n, f);
      fclose(f);
      WebPFree(o);
    }
    return 0;
  }
  if (!strcmp(mode, "latency")) {
    static double ts[1000];
    for (int i = 0; i < nin; i++) {
      run(&ins[i], NULL);
      run(&ins[i], NULL);
      int n = 0;
      double start = now_ns();
      while (n < 5 || (now_ns() - start < 300e6 && n < 1000)) {
        double t = now_ns();
        run(&ins[i], NULL);
        ts[n++] = now_ns() - t;
      }
      double total = now_ns() - start;
      qsort(ts, n, sizeof *ts, cmpd);
#define PCT(p) ts[(int)fmin(n - 1, ceil((p) * n) - 1)]
      printf("Benchmark%s/impl=%s/img=%s/cpus=%d\t%d\t%.0f ns/op\t%.0f p50-ns\t%.0f p95-ns\t%.0f p99-ns\n", op, impl,
             ins[i].name, cpus, n, total / n, PCT(0.50), PCT(0.95), PCT(0.99));
    }
    return 0;
  }
  // throughput: cpus workers for one second
  for (int i = 0; i < nin; i++) run(&ins[i], NULL);
  pthread_t th[64];
  double t0 = now_ns();
  for (long w = 0; w < cpus; w++) pthread_create(&th[w], NULL, worker, (void*)w);
  struct timespec one = {1, 0};
  nanosleep(&one, NULL);
  stop = 1;
  long total = 0;
  for (int w = 0; w < cpus; w++) {
    pthread_join(th[w], NULL);
    total += counts[w];
  }
  double wall = now_ns() - t0;
  printf("Benchmark%sThroughput/impl=%s/cpus=%d\t%ld\t%.0f ns/op\t%.2f img/s\t%.1f peak-RSS-MB\n", op, impl, cpus,
         total, wall / total, total / (wall / 1e9), rss_mb());
  return 0;
}
