package webp_test

import (
	"bytes"
	"image"
	"io"
	"runtime"
	"testing"

	"github.com/jotadrilo/webp"
	"github.com/jotadrilo/webp/internal/testimg"
)

// thumb is the benchmark's reference workload: a 384 x 256 photo-like thumbnail.
func thumb() *image.NRGBA { return testimg.New("photo", 384, 256) }

func BenchmarkEncode(b *testing.B) {
	m := thumb()
	for _, c := range []struct {
		name string
		o    *webp.Options
	}{
		{"m0", &webp.Options{Quality: 80, Method: 0}},
		{"m4", &webp.Options{Quality: 80, Method: 4}},
		{"lossless", &webp.Options{Lossless: true, Quality: 75, Method: 4}},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := webp.Encode(io.Discard, m, c.o); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	m := thumb()
	for _, c := range []struct {
		name string
		o    *webp.Options
	}{
		{"lossy", &webp.Options{Quality: 80, Method: 4}},
		{"lossless", &webp.Options{Lossless: true, Quality: 75, Method: 4}},
	} {
		var buf bytes.Buffer
		if err := webp.Encode(&buf, m, c.o); err != nil {
			b.Fatal(err)
		}
		data := buf.Bytes()
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := webp.Decode(bytes.NewReader(data)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// allocated returns the bytes and objects f allocates per call, after warming up the pool.
func allocated(f func()) (bytesPerOp, objsPerOp float64) {
	f()
	f()
	const n = 20
	var a, z runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	for i := 0; i < n; i++ {
		f()
	}
	runtime.ReadMemStats(&z)
	return float64(z.TotalAlloc-a.TotalAlloc) / n, float64(z.Mallocs-a.Mallocs) / n
}

// TestAllocations keeps a steady-state 384 px encode under ~50 KB and a few objects, and a
// decode at the decoded image plus a little: the pixels live in the pooled module's memory.
func TestAllocations(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("allocation counts are not meaningful under -race or -short")
	}
	m := thumb()
	for _, o := range []*webp.Options{{Quality: 80}, {Quality: 80, Method: 4}, {Lossless: true}} {
		bytes, objs := allocated(func() { _ = webp.Encode(io.Discard, m, o) })
		t.Logf("encode %+v: %.0f B, %.1f objects per call", *o, bytes, objs)
		if bytes > 50<<10 || objs > 20 {
			t.Errorf("encode %+v allocates %.0f B in %.1f objects per call; want <= 50 KB, 20", *o, bytes, objs)
		}
	}
	var buf bytesBuffer
	_ = webp.Encode(&buf, m, nil)
	img := len(m.Pix)
	bytes, objs := allocated(func() { _, _ = webp.Decode(buf.reader()) })
	t.Logf("decode: %.0f B, %.1f objects per call (image %d B)", bytes, objs, img)
	if bytes > float64(img)+16<<10 || objs > 20 {
		t.Errorf("decode allocates %.0f B in %.1f objects; want <= image + 16 KB, 20", bytes, objs)
	}
}

type bytesBuffer struct{ bytes.Buffer }

func (b *bytesBuffer) reader() io.Reader { return bytes.NewReader(b.Bytes()) }
