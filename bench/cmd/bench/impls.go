package main

import (
	"bytes"
	"image"
	"io"

	"github.com/eringen/gowebper"
	vpx "github.com/gen2brain/vpx/webp"
	gen2brain "github.com/gen2brain/webp"
	ours "github.com/jotadrilo/webp"
	xwebp "golang.org/x/image/webp"
)

// An encoder under test, at one setting. Group names the comparable setting (the C baseline of
// the same group is the ratio's denominator).
type encoder struct {
	group string
	enc   func(io.Writer, *image.NRGBA) error
	// multicore: the implementation may use more than one core for one image.
	multicore bool
}

type decoder struct {
	dec       func([]byte) (image.Image, error)
	multicore bool
}

// The settings compared: lossy quality 80 at method 0 (thumbnails) and 4 (libwebp's default), and
// lossless at each package's default effort.
var encoders = map[string]encoder{
	"ours-m0": {"lossy-m0", func(w io.Writer, m *image.NRGBA) error {
		return ours.Encode(w, m, &ours.Options{Quality: 80, Method: 0})
	}, false},
	"ours-m4": {"lossy-m4", func(w io.Writer, m *image.NRGBA) error {
		return ours.Encode(w, m, &ours.Options{Quality: 80, Method: 4})
	}, false},
	"ours-lossless": {"lossless", func(w io.Writer, m *image.NRGBA) error {
		return ours.Encode(w, m, &ours.Options{Lossless: true, Quality: 75, Method: 4})
	}, false},
	"gen2brain-m0": {"lossy-m0", func(w io.Writer, m *image.NRGBA) error {
		return gen2brain.Encode(w, m, gen2brain.Options{Quality: 80, Method: 0})
	}, false},
	"gen2brain-m4": {"lossy-m4", func(w io.Writer, m *image.NRGBA) error {
		return gen2brain.Encode(w, m, gen2brain.Options{Quality: 80, Method: 4})
	}, false},
	"gen2brain-lossless": {"lossless", func(w io.Writer, m *image.NRGBA) error {
		return gen2brain.Encode(w, m, gen2brain.Options{Lossless: true, Quality: 75, Method: 4})
	}, false},
	// vpx: Threads 0 = GOMAXPROCS goroutines per lossy frame (its default).
	"vpx-m0": {"lossy-m0", func(w io.Writer, m *image.NRGBA) error {
		return vpx.Encode(w, m, vpx.EncodeOptions{Quality: 80, Method: 0})
	}, true},
	"vpx-m4": {"lossy-m4", func(w io.Writer, m *image.NRGBA) error {
		return vpx.Encode(w, m, vpx.EncodeOptions{Quality: 80, Method: 4})
	}, true},
	"vpx-lossless": {"lossless", func(w io.Writer, m *image.NRGBA) error {
		return vpx.Encode(w, m, vpx.EncodeOptions{Lossless: true, Method: 4})
	}, false},
	// gowebper is lossless only.
	"gowebper-lossless": {"lossless", func(w io.Writer, m *image.NRGBA) error {
		return gowebper.Encode(w, m, &gowebper.Options{Level: gowebper.LevelDefault})
	}, false},
}

var decoders = map[string]decoder{
	"ours":      {func(b []byte) (image.Image, error) { return ours.Decode(bytes.NewReader(b)) }, false},
	"gen2brain": {func(b []byte) (image.Image, error) { return gen2brain.Decode(bytes.NewReader(b)) }, false},
	"vpx":       {func(b []byte) (image.Image, error) { return vpx.Decode(bytes.NewReader(b)) }, false},
	"ximage":    {func(b []byte) (image.Image, error) { return xwebp.Decode(bytes.NewReader(b)) }, false},
}
