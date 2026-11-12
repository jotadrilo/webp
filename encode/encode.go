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
		stride := wd * 4
		p := i.m.Xmalloc(int32(stride * ht))
		if p == 0 {
			return &werr.Error{Op: "encode", Err: ErrOutOfMemory}
		}
		opaque := importPixels(i.m.Mem()[uint32(p):uint32(p)+uint32(stride*ht)], m, wd, ht)
		i.w = w
		code := i.m.Xwp_encode(p, int32(wd), int32(ht), int32(stride), o.Quality, int32(o.Method),
			b2i(o.Lossless), b2i(o.Exact), b2i(opaque)) // frees p
		if i.werr != nil {
			return i.werr
		}
		if code != 0 {
			return werr.Encode(code)
		}
		return nil
	})
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// importPixels writes m as tightly packed non-premultiplied RGBA into dst and reports whether
// every pixel is known to be opaque.
func importPixels(dst []byte, m image.Image, wd, ht int) (opaque bool) {
	stride := wd * 4
	switch m := m.(type) {
	case *image.NRGBA:
		copyRows(dst, m.Pix[m.PixOffset(m.Rect.Min.X, m.Rect.Min.Y):], m.Stride, stride, ht)
		return false
	case *image.RGBA:
		src := m.Pix[m.PixOffset(m.Rect.Min.X, m.Rect.Min.Y):]
		copyRows(dst, src, m.Stride, stride, ht)
		return unpremultiply(dst)
	case *image.Paletted:
		var pal [256][4]byte
		for i, c := range m.Palette[:min(len(m.Palette), 256)] {
			n := color.NRGBAModel.Convert(c).(color.NRGBA)
			pal[i] = [4]byte{n.R, n.G, n.B, n.A}
		}
		w := m.Rect.Dx()
		for y := 0; y < ht; y++ {
			src := m.Pix[m.PixOffset(m.Rect.Min.X, m.Rect.Min.Y+y):][:w]
			row := dst[y*stride : (y+1)*stride]
			for x, c := range src {
				copy(row[x*4:x*4+4], pal[c][:])
			}
		}
		return false
	}
	b := m.Bounds()
	if o, ok := m.(interface{ Opaque() bool }); ok && o.Opaque() {
		// Opaque: image/draw converts into an RGBA view of dst, with fast paths for YCbCr, Gray
		// and others; premultiplied and non-premultiplied are the same bytes when opaque.
		view := &image.RGBA{Pix: dst, Stride: stride, Rect: image.Rect(0, 0, wd, ht)}
		draw.Draw(view, view.Rect, m, b.Min, draw.Src)
		return true
	}
	// Anything else, pixel by pixel through color.NRGBAModel, which keeps 16-bit precision.
	for y := 0; y < ht; y++ {
		row := dst[y*stride:]
		for x := 0; x < wd; x++ {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.R, c.G, c.B, c.A
		}
	}
	return false
}

func copyRows(dst, src []byte, srcStride, rowBytes, rows int) {
	if srcStride == rowBytes {
		copy(dst[:rowBytes*rows], src)
		return
	}
	for y := 0; y < rows; y++ {
		copy(dst[y*rowBytes:(y+1)*rowBytes], src[y*srcStride:])
	}
}

// unpremultiply converts premultiplied RGBA in p to non-premultiplied in place, exactly like
// color.NRGBAModel, and reports whether every pixel was opaque.
func unpremultiply(p []byte) (opaque bool) {
	opaque = true
	for i := 0; i+3 < len(p); i += 4 {
		a := p[i+3]
		if a == 0xff {
			continue
		}
		opaque = false
		if a == 0 {
			continue // premultiplied (0,0,0,0): already the NRGBA value color.NRGBAModel gives
		}
		a16 := uint32(a) * 0x101
		p[i] = uint8((uint32(p[i]) * 0x101 * 0xffff / a16) >> 8)
		p[i+1] = uint8((uint32(p[i+1]) * 0x101 * 0xffff / a16) >> 8)
		p[i+2] = uint8((uint32(p[i+2]) * 0x101 * 0xffff / a16) >> 8)
	}
	return opaque
}
