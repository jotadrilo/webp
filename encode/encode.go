// Package encode is the encoding half of github.com/jotadrilo/webp: Google's libwebp encoder in
// pure Go. Import it instead of the webp package when a program only encodes, so that the
// decoder is not linked in (and the "webp" image format is not registered).
package encode

import (
	"image"
	"image/color"
	"image/draw"
	"io"

	"github.com/jotadrilo/webp/internal/werr"
)

// The errors Encode returns (see the webp package; the values are the same).
var (
	ErrInvalidImage   = werr.ErrInvalidImage
	ErrInvalidOptions = werr.ErrInvalidOptions
	ErrOutOfMemory    = werr.ErrOutOfMemory
	ErrInternal       = werr.ErrInternal
)

// Error is the error type of this package.
type Error = werr.Error

// MaxDimension is the largest width or height a WebP image can have.
const MaxDimension = 16383

// Options are the encoding parameters. A nil *Options means DefaultOptions(). A non-nil
// Options is used as given (so start from DefaultOptions() when changing one field).
type Options struct {
	// Lossless selects the lossless format (VP8L) instead of lossy (VP8).
	Lossless bool
	// Quality, from 0 to 100. Lossy: 0 is the smallest file, 100 the best quality.
	// Lossless: the effort, 0 is the fastest, 100 the smallest file. libwebp's default is 75.
	Quality float32
	// Method, from 0 (fastest) to 6 (slowest, smallest). libwebp's default is 4. Small images
	// such as thumbnails often want 0: about 3x faster than 4, for files ~20 % larger.
	Method int
	// Exact keeps the RGB values under fully transparent pixels. By default libwebp replaces
	// them to compress better (the pixels stay fully transparent either way).
	Exact bool
}

// DefaultOptions returns libwebp's defaults: lossy, Quality 75, Method 4.
func DefaultOptions() *Options { return &Options{Quality: 75, Method: 4} }

var defaultOptions = DefaultOptions()

// Encode writes m to w in WebP format with libwebp, with options o (nil for the defaults).
// Alpha is kept exactly in both formats: lossy files carry it losslessly. *image.NRGBA and
// *image.RGBA (and *image.Paletted) are copied straight into libwebp's memory; opaque images of
// other types are converted there with image/draw, the rest through color.NRGBAModel.
func Encode(w io.Writer, m image.Image, o *Options) error {
	if o == nil {
		o = defaultOptions
	}
	if !(o.Quality >= 0 && o.Quality <= 100) || o.Method < 0 || o.Method > 6 {
		return &werr.Error{Op: "encode", Err: ErrInvalidOptions}
	}
	b := m.Bounds()
	wd, ht := b.Dx(), b.Dy()
	if wd <= 0 || ht <= 0 || wd > MaxDimension || ht > MaxDimension {
		return &werr.Error{Op: "encode", Err: ErrInvalidImage}
	}
	return call(func(i *instance) error {
		reserve(i, wd, ht, o.Lossless)
		pic := i.m.Xwp_picture(int32(wd), int32(ht), b2i(o.Lossless))
		if pic == 0 {
			return &werr.Error{Op: "encode", Code: 1, Err: ErrOutOfMemory}
		}
		if o.Lossless {
			// The pixels go straight into libwebp's ARGB picture (BGRA bytes), strip by strip.
			argb, stride := uint32(i.m.Xwp_argb(pic)), int(i.m.Xwp_argb_stride(pic))*4
			for y0 := 0; y0 < ht; y0 += stripRows {
				rows := min(stripRows, ht-y0)
				dst := i.m.Mem()[argb+uint32(y0*stride) : argb+uint32((y0+rows)*stride)]
				importRows(dst, stride, m, y0, rows)
				swapRB(dst)
			}
		} else {
			// Strip by strip through a small RGBA buffer, converted by libwebp's own import.
			buf := i.m.Xmalloc(int32(stripRows * wd * 4))
			if buf == 0 {
				i.m.Xwp_picture_free(pic)
				return &werr.Error{Op: "encode", Code: 1, Err: ErrOutOfMemory}
			}
			for y0 := 0; y0 < ht; y0 += stripRows {
				rows := min(stripRows, ht-y0)
				dst := i.m.Mem()[uint32(buf) : uint32(buf)+uint32(rows*wd*4)]
				opaque := importRows(dst, wd*4, m, y0, rows)
				if code := i.m.Xwp_import_rows(pic, buf, int32(wd*4), int32(y0), int32(rows), b2i(opaque)); code != 0 {
					i.m.Xfree(buf)
					i.m.Xwp_picture_free(pic)
					return werr.Encode(code)
				}
			}
			i.m.Xfree(buf)
		}
		i.w = w
		code := i.m.Xwp_encode(pic, o.Quality, int32(o.Method), b2i(o.Lossless), b2i(o.Exact)) // frees pic
		if i.werr != nil {
			return i.werr
		}
		if code != 0 {
			return werr.Encode(code)
		}
		return nil
	})
}

// stripRows is the height of the strips the pixels are imported in (even: libwebp converts
// RGB to YUV two rows at a time).
const stripRows = 32

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// importRows writes rows [y0, y0+rows) of m (relative to its bounds) as non-premultiplied
// RGBA into dst, stride bytes per row, and reports whether they are all opaque.
func importRows(dst []byte, stride int, m image.Image, y0, rows int) (opaque bool) {
	b := m.Bounds()
	wd := b.Dx()
	switch m := m.(type) {
	case *image.NRGBA:
		copyRows(dst, m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y0):], m.Stride, stride, wd*4, rows)
		return opaqueRows(dst, stride, wd, rows)
	case *image.RGBA:
		copyRows(dst, m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y0):], m.Stride, stride, wd*4, rows)
		return unpremultiply(dst, stride, wd, rows)
	case *image.Paletted:
		var pal [256][4]byte
		for i, c := range m.Palette[:min(len(m.Palette), 256)] {
			n := color.NRGBAModel.Convert(c).(color.NRGBA)
			pal[i] = [4]byte{n.R, n.G, n.B, n.A}
		}
		opaque = true
		for y := 0; y < rows; y++ {
			src := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y0+y):][:wd]
			row := dst[y*stride : y*stride+wd*4]
			for x, c := range src {
				copy(row[x*4:x*4+4], pal[c][:])
				opaque = opaque && pal[c][3] == 0xff
			}
		}
		return opaque
	}
	if o, ok := m.(interface{ Opaque() bool }); ok && o.Opaque() {
		// Opaque: image/draw converts into an RGBA view of dst, with fast paths for YCbCr, Gray
		// and others; premultiplied and non-premultiplied are the same bytes when opaque.
		view := &image.RGBA{Pix: dst, Stride: stride, Rect: image.Rect(0, 0, wd, rows)}
		draw.Draw(view, view.Rect, m, image.Pt(b.Min.X, b.Min.Y+y0), draw.Src)
		return true
	}
	// Anything else, pixel by pixel through color.NRGBAModel, which keeps 16-bit precision.
	opaque = true
	for y := 0; y < rows; y++ {
		row := dst[y*stride:]
		for x := 0; x < wd; x++ {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y0+y)).(color.NRGBA)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.R, c.G, c.B, c.A
			opaque = opaque && c.A == 0xff
		}
	}
	return opaque
}

func opaqueRows(p []byte, stride, wd, rows int) bool {
	for y := 0; y < rows; y++ {
		row := p[y*stride : y*stride+wd*4]
		for i := 3; i < len(row); i += 4 {
			if row[i] != 0xff {
				return false
			}
		}
	}
	return true
}

// swapRB turns RGBA into BGRA (libwebp's ARGB words, little endian) in place.
func swapRB(p []byte) {
	for i := 0; i+3 < len(p); i += 4 {
		p[i], p[i+2] = p[i+2], p[i]
	}
}

func copyRows(dst, src []byte, srcStride, dstStride, rowBytes, rows int) {
	if srcStride == rowBytes && dstStride == rowBytes {
		copy(dst[:rowBytes*rows], src[:rowBytes*rows])
		return
	}
	for y := 0; y < rows; y++ {
		copy(dst[y*dstStride:y*dstStride+rowBytes], src[y*srcStride:y*srcStride+rowBytes])
	}
}

// unpremultiply converts premultiplied RGBA rows in p to non-premultiplied in place, exactly
// like color.NRGBAModel, and reports whether every pixel was opaque.
func unpremultiply(p []byte, stride, wd, rows int) (opaque bool) {
	opaque = true
	for y := 0; y < rows; y++ {
		row := p[y*stride : y*stride+wd*4]
		for i := 0; i+3 < len(row); i += 4 {
			a := row[i+3]
			if a == 0xff {
				continue
			}
			opaque = false
			if a == 0 {
				continue // premultiplied (0,0,0,0): already the NRGBA value color.NRGBAModel gives
			}
			a16 := uint32(a) * 0x101
			row[i] = uint8((uint32(row[i]) * 0x101 * 0xffff / a16) >> 8)
			row[i+1] = uint8((uint32(row[i+1]) * 0x101 * 0xffff / a16) >> 8)
			row[i+2] = uint8((uint32(row[i+2]) * 0x101 * 0xffff / a16) >> 8)
		}
	}
	return opaque
}

// reserve grows the module's memory once to what an encode of a wd x ht image needs at its
// peak, by allocating that much and freeing it: the allocator then serves the encode from that
// free space. Without it, libwebp's allocations grow the memory step by step, and every step
// reallocates the whole memory, leaving the old copy as garbage until the next GC: a 24 MP
// lossy encode peaked 85 MB higher, a lossless one 2.4 GB higher.
func reserve(i *instance, wd, ht int, lossless bool) {
	px := wd * ht
	need := px*5/2 + 1<<20 // the Y, U, V, A planes and the encoder's state
	if lossless {
		// libwebp's lossless encoder peaks at ~30 bytes per pixel (hash chain, backward
		// references, histograms) plus ~24 MB that does not depend on the size.
		need = px*32 + 24<<20
	}
	if need <= 4<<20 || len(i.m.Mem()) >= need {
		return
	}
	if p := i.m.Xmalloc(int32(need)); p != 0 {
		i.m.Xfree(p)
	}
}
