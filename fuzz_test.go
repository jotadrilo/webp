package webp_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jotadrilo/webp"
)

// FuzzDecode feeds arbitrary bytes to DecodeConfig and Decode: they must return an image or a
// *webp.Error, never panic, and agree with each other. Seeds: the fixtures, whole and cut.
func FuzzDecode(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.webp")
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
		f.Add(b[:len(b)/3])
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		cfg, cerr := webp.DecodeConfig(bytes.NewReader(b))
		if cerr != nil {
			var we *webp.Error
			if !errors.As(cerr, &we) {
				t.Fatalf("DecodeConfig: %T %v is not a *webp.Error", cerr, cerr)
			}
		}
		if cerr == nil && cfg.Width*cfg.Height > 1<<22 {
			return // a valid header asking for a huge image: real, but not what fuzzing is for
		}
		m, err := webp.Decode(bytes.NewReader(b))
		if err != nil {
			var we *webp.Error
			if !errors.As(err, &we) || m != nil {
				t.Fatalf("Decode: %T %v (image %v)", err, err, m)
			}
			return
		}
		if cerr != nil {
			t.Fatalf("Decode succeeded where DecodeConfig failed: %v", cerr)
		}
		if r := m.Bounds(); r.Dx() != cfg.Width || r.Dy() != cfg.Height || m.ColorModel() != cfg.ColorModel {
			t.Fatalf("image %v %v, config %+v", r, m.ColorModel(), cfg)
		}
	})
}
