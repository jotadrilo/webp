#!/usr/bin/env bash
# Builds libwebp's own dwebp and cwebp natively (with libwebp's SIMD: NEON / SSE2) from the
# pinned source, without PNG/JPEG/TIFF support (PAM in and out is enough), into .cache/bin.
# Tests and the benchmark use dwebp as the reference decoder.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
"$root/scripts/fetch-libwebp.sh" >&2
L=$root/third_party/libwebp/_src
bin=$root/.cache/bin
mkdir -p "$bin"
lib=$(cd "$L" && ls src/dec/*.c src/enc/*.c src/dsp/*.c src/utils/*.c src/demux/*.c sharpyuv/*.c)
cd "$L"
# shellcheck disable=SC2086
${CC:-cc} -O2 -w -DNDEBUG -I"$L/src" -I"$L" -o "$bin/dwebp" examples/dwebp.c examples/example_util.c \
  imageio/image_enc.c imageio/imageio_util.c imageio/webpdec.c imageio/metadata.c $lib -lm -lpthread
# shellcheck disable=SC2086
${CC:-cc} -O2 -w -DNDEBUG -I"$L/src" -I"$L" -o "$bin/cwebp" examples/cwebp.c examples/example_util.c \
  imageio/image_dec.c imageio/imageio_util.c imageio/webpdec.c imageio/metadata.c imageio/pnmdec.c \
  imageio/pngdec.c imageio/jpegdec.c imageio/tiffdec.c imageio/wicdec.c $lib -lm -lpthread
"$bin/dwebp" -version >&2
echo "$bin/dwebp"
