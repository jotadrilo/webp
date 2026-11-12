// Package decode is the decoding half of github.com/jotadrilo/webp: Google's libwebp decoder
// in pure Go. Import it instead of the webp package when a program only decodes, so that the
// encoder is not linked in. Importing it registers the "webp" format with the image package.
package decode

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"

	"github.com/jotadrilo/webp/internal/gen/dec"
	"github.com/jotadrilo/webp/internal/pool"
	"github.com/jotadrilo/webp/internal/werr"
)

// The errors Decode and DecodeConfig return (see the webp package; the values are the same).
var (
	ErrFormat      = werr.ErrFormat
	ErrTruncated   = werr.ErrTruncated
	ErrUnsupported = werr.ErrUnsupported
	ErrOutOfMemory = werr.ErrOutOfMemory
	ErrInternal    = werr.ErrInternal
)

// Error is the error type of this package.
type Error = werr.Error

// An instance is one libwebp decoder module.
type instance struct{ m *dec.Module }

var instances = pool.New(func() *instance {
	i := new(instance)
	i.m = dec.New(i)
	i.m.X_initialize()
	return i
}, func(i *instance) int { return len(i.m.Mem()) })

// Xrandom_get is the WASI import wasi-libc uses to seed its stack-protector canary. The
// module's memory is a bounds-checked Go slice, so the canary protects nothing here; a fixed
// value keeps every instance deterministic.
func (i *instance) Xrandom_get(p, n int32) int32 {
	clear(i.m.Mem()[uint32(p) : uint32(p)+uint32(n)])
	return 0
}

// call runs f on a pooled instance. A panic inside the module (a trap) discards the instance
// and becomes ErrInternal, so bad input can never crash the caller.
func call(op string, f func(*instance) error) (err error) {
	i := instances.Get()
	defer func() {
		if r := recover(); r != nil {
			err = &werr.Error{Op: op, Err: werr.ErrInternal}
			return
		}
		instances.Put(i)
	}()
	return f(i)
}

func init() {
	image.RegisterFormat("webp", "RIFF????WEBPVP8", Decode, DecodeConfig)
}

// features is libwebp's WebPBitstreamFeatures.
type features struct {
	width, height int
	alpha, anim   bool
}

// Decode reads a WebP image from r and decodes it with libwebp, giving exactly the pixels of
// libwebp's dwebp (and browsers built on libwebp). It returns an *image.NRGBA when the image
// has alpha and an *image.RGBA (opaque, so the same bytes) when it has not.
//
// Animated WebP is not supported (ErrUnsupported). Like image/png, Decode allocates what the
// header asks for (up to 16383 x 16383 x 4 bytes); check DecodeConfig first for untrusted input
// whose size matters to you.
func Decode(r io.Reader) (image.Image, error) {
	var out image.Image
	err := call("decode", func(i *instance) error {
		p, n, err := readInto(i, r)
		if err != nil {
			return err
		}
		defer i.m.Xfree(p)
		f, err := getFeatures(i, "decode", p, n)
		if err != nil {
			return err
		}
		if f.anim {
			return &werr.Error{Op: "decode", Code: werr.StatusUnsupportedFeature, Err: ErrUnsupported}
		}
		stride := f.width * 4
		size := stride * f.height
		q := i.m.Xmalloc(int32(size))
		if q == 0 {
			return &werr.Error{Op: "decode", Code: werr.StatusOutOfMemory, Err: ErrOutOfMemory}
		}
		defer i.m.Xfree(q)
		if st := i.m.Xwp_decode(p, n, q, int32(size), int32(stride)); st != werr.StatusOK {
			return werr.Decode("decode", st)
		}
		pix := make([]byte, size)
		copy(pix, i.m.Mem()[uint32(q):uint32(q)+uint32(size)])
		rect := image.Rect(0, 0, f.width, f.height)
		if f.alpha {
			out = &image.NRGBA{Pix: pix, Stride: stride, Rect: rect}
		} else {
			out = &image.RGBA{Pix: pix, Stride: stride, Rect: rect}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DecodeConfig returns the dimensions and color model of a WebP image (color.NRGBAModel with
// alpha, color.RGBAModel without), reading only its headers.
func DecodeConfig(r io.Reader) (image.Config, error) {
	var cfg image.Config
	err := call("decode config", func(i *instance) error {
		// The features live in the first 30 bytes of a RIFF file; otherwise read more until
		// libwebp has enough.
		buf := make([]byte, 0, 64)
		for {
			k, err := io.ReadFull(r, buf[len(buf):cap(buf)])
			buf = buf[:len(buf)+k]
			p := i.m.Xmalloc(int32(len(buf)))
			if p == 0 {
				return &werr.Error{Op: "decode config", Err: ErrOutOfMemory}
			}
			copy(i.m.Mem()[uint32(p):], buf)
			f, ferr := getFeatures(i, "decode config", p, int32(len(buf)))
			i.m.Xfree(p)
			if ferr == nil {
				cfg = image.Config{Width: f.width, Height: f.height, ColorModel: color.RGBAModel}
				if f.alpha {
					cfg.ColorModel = color.NRGBAModel
				}
				return nil
			}
			// More data can turn any failure into success (raw VP8 bitstreams are only checked
			// against the bytes given), so read on until the end of the stream.
			if err != nil || len(buf) >= 1<<20 {
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
					return err
				}
				return ferr
			}
			buf = append(buf, make([]byte, len(buf))...)
		}
	})
	return cfg, err
}

func getFeatures(i *instance, op string, p, n int32) (features, error) {
	o := i.m.Xmalloc(20)
	if o == 0 {
		return features{}, &werr.Error{Op: op, Code: werr.StatusOutOfMemory, Err: ErrOutOfMemory}
	}
	defer i.m.Xfree(o)
	if st := i.m.Xwp_features(p, n, o); st != werr.StatusOK {
		return features{}, werr.Decode(op, st)
	}
	v := i.m.Mem()[uint32(o) : uint32(o)+20]
	le := binary.LittleEndian
	return features{
		width:  int(int32(le.Uint32(v[0:]))),
		height: int(int32(le.Uint32(v[4:]))),
		alpha:  le.Uint32(v[8:]) != 0,
		anim:   le.Uint32(v[12:]) != 0,
	}, nil
}

// maxInput bounds the WebP files Decode reads (a RIFF file is at most 4 GiB).
const maxInput = 1<<31 - 1

// readInto reads the whole WebP stream from r straight into the module's memory, without an
// intermediate Go buffer. A RIFF header gives the size up front; the buffer still grows
// gradually so that a header claiming more data than r holds cannot force a large allocation.
func readInto(i *instance, r io.Reader) (p, n int32, err error) {
	var hdr [12]byte
	k, err := io.ReadFull(r, hdr[:])
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, 0, &werr.Error{Op: "decode", Code: werr.StatusNotEnoughData, Err: ErrTruncated}
		}
		return 0, 0, err
	}
	want := int64(-1) // unknown: read to EOF
	if string(hdr[0:4]) == "RIFF" && string(hdr[8:12]) == "WEBP" {
		want = 8 + int64(binary.LittleEndian.Uint32(hdr[4:8]))
		want += want & 1 // RIFF chunks are padded to an even size
		want = min(want, maxInput)
	}
	capacity := int64(64 << 10)
	if want >= 0 {
		capacity = min(max(want, 12), capacity)
	}
	p = i.m.Xmalloc(int32(capacity))
	if p == 0 {
		return 0, 0, &werr.Error{Op: "decode", Code: werr.StatusOutOfMemory, Err: ErrOutOfMemory}
	}
	copy(i.m.Mem()[uint32(p):], hdr[:k])
	size := int64(k)
	for want < 0 || size < want {
		if size == capacity {
			next := min(capacity*2, maxInput)
			if want >= 0 {
				next = min(next, want)
			}
			if next == capacity {
				break
			}
			q := i.m.Xrealloc(p, int32(next))
			if q == 0 {
				i.m.Xfree(p)
				return 0, 0, &werr.Error{Op: "decode", Code: werr.StatusOutOfMemory, Err: ErrOutOfMemory}
			}
			p, capacity = q, next
		}
		end := capacity
		if want >= 0 {
			end = min(end, want)
		}
		m, rerr := r.Read(i.m.Mem()[uint32(p)+uint32(size) : uint32(p)+uint32(end)])
		size += int64(m)
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			i.m.Xfree(p)
			return 0, 0, rerr
		}
	}
	return p, int32(size), nil
}
