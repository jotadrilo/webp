package webp_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jotadrilo/webp"
	"github.com/jotadrilo/webp/internal/testimg"
)

// The cwebp settings of each fixture variant (see internal/testimg/mktestdata).
var variants = map[string]*webp.Options{
	"q75":      {Quality: 75, Method: 4},
	"q80m0":    {Quality: 80, Method: 0},
	"lossless": {Lossless: true, Quality: 75, Method: 4},
}

func readPNG(t testing.TB, path string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	m, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return toNRGBA(m)
}

func toNRGBA(m image.Image) *image.NRGBA {
	if n, ok := m.(*image.NRGBA); ok {
		return n
	}
	b := m.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if r, ok := m.(*image.RGBA); ok && r.Opaque() {
		for y := 0; y < b.Dy(); y++ {
			copy(n.Pix[y*n.Stride:], r.Pix[r.PixOffset(b.Min.X, b.Min.Y+y):][:b.Dx()*4])
		}
		return n
	}
	draw.Draw(n, n.Rect, m, b.Min, draw.Src)
	return n
}

// diff returns the largest per-channel difference between a and b, or -1 if their sizes differ.
// With visibleOnly, the RGB of pixels whose alpha is 0 in both is ignored.
func diff(a, b *image.NRGBA, visibleOnly bool) int {
	if a.Rect.Dx() != b.Rect.Dx() || a.Rect.Dy() != b.Rect.Dy() {
		return -1
	}
	d := 0
	w, h := a.Rect.Dx(), a.Rect.Dy()
	for y := 0; y < h; y++ {
		pa := a.Pix[a.PixOffset(a.Rect.Min.X, a.Rect.Min.Y+y):]
		pb := b.Pix[b.PixOffset(b.Rect.Min.X, b.Rect.Min.Y+y):]
		for i := 0; i < w*4; i += 4 {
			first := 0
			if visibleOnly && pa[i+3] == 0 && pb[i+3] == 0 {
				first = 3
			}
			for c := first; c < 4; c++ {
				d = max(d, absDiff(pa[i+c], pb[i+c]))
			}
		}
	}
	return d
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func fixtures(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/*.webp")
	if err != nil || len(files) == 0 {
		t.Fatal("no fixtures in testdata/; run make testdata", err)
	}
	return files
}

// TestDecodeMatchesDwebp decodes every fixture (made by libwebp's cwebp) and compares it with
// what libwebp's dwebp decoded it to: lossy and lossless, the pixels must be identical.
func TestDecodeMatchesDwebp(t *testing.T) {
	for _, f := range fixtures(t) {
		name := strings.TrimSuffix(filepath.Base(f), ".webp")
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			m, err := webp.Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			want := readPNG(t, filepath.Join("testdata", "golden", name+".png"))
			alpha := !testimg.Opaque(want)
			if _, isN := m.(*image.NRGBA); isN != alpha {
				t.Errorf("got %T for an image with alpha=%v", m, alpha)
			}
			if d := diff(toNRGBA(m), want, false); d != 0 {
				t.Errorf("max difference from dwebp %d, want 0", d)
			}
		})
	}
}

// TestEncodeMatchesCwebp encodes every fixture's source image with the fixture's settings: the
// output must be the bytes libwebp's cwebp wrote.
func TestEncodeMatchesCwebp(t *testing.T) {
	for _, f := range fixtures(t) {
		name := strings.TrimSuffix(filepath.Base(f), ".webp")
		kind, variant := name[:strings.LastIndex(name, "-")], name[strings.LastIndex(name, "-")+1:]
		t.Run(name, func(t *testing.T) {
			src := readPNG(t, filepath.Join("testdata", kind+".png"))
			want, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if err := webp.Encode(&got, src, variants[variant]); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("output differs from cwebp: %d bytes, cwebp %d", got.Len(), len(want))
			}
		})
	}
}

func encode(t testing.TB, m image.Image, o *webp.Options) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := webp.Encode(&b, m, o); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t testing.TB, b []byte) *image.NRGBA {
	t.Helper()
	m, err := webp.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return toNRGBA(m)
}

// TestRoundTrip encodes every kind of image at odd sizes and decodes it back.
func TestRoundTrip(t *testing.T) {
	for _, kind := range testimg.Kinds {
		for _, sz := range [][2]int{{1, 1}, {3, 2}, {37, 61}, {200, 150}} {
			if testing.Short() && sz[0] > 64 {
				continue
			}
			src := testimg.New(kind, sz[0], sz[1])
			t.Run(fmt.Sprintf("%s-%dx%d", kind, sz[0], sz[1]), func(t *testing.T) {
				// Lossless: identical, and with Exact even the RGB under alpha 0.
				if d := diff(decode(t, encode(t, src, &webp.Options{Lossless: true, Exact: true})), src, false); d != 0 {
					t.Errorf("lossless exact: max difference %d", d)
				}
				if d := diff(decode(t, encode(t, src, &webp.Options{Lossless: true, Quality: 30, Method: 1})), src, true); d != 0 {
					t.Errorf("lossless: max visible difference %d", d)
				}
				// Lossy: alpha is still exact; the colors are close.
				got := decode(t, encode(t, src, nil))
				for i := 3; i < len(src.Pix); i += 4 {
					if got.Pix[i] != src.Pix[i] {
						t.Fatalf("lossy: alpha differs at byte %d: %d, want %d", i, got.Pix[i], src.Pix[i])
					}
				}
			})
		}
	}
}

// dwebp returns the path to libwebp's dwebp: $DWEBP, or the one `make dwebp` builds.
func dwebp(t testing.TB) string {
	if p := os.Getenv("DWEBP"); p != "" {
		return p
	}
	if p, err := filepath.Abs(".cache/bin/dwebp"); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatal("dwebp not found: run make dwebp")
	}
	t.Skip("dwebp not found: run make dwebp")
	return ""
}

// TestEncodeDecodeWithDwebp encodes with this package and decodes with libwebp's native dwebp:
// lossless gives back the source, and lossy gives exactly what Decode gives.
func TestEncodeDecodeWithDwebp(t *testing.T) {
	bin := dwebp(t)
	dir := t.TempDir()
	opts := map[string]*webp.Options{
		"default": nil, "q90m6": {Quality: 90, Method: 6}, "q50m0": {Quality: 50, Method: 0},
		"lossless": {Lossless: true, Quality: 50, Method: 2}, "exact": {Lossless: true, Exact: true, Quality: 10},
	}
	w, h := 157, 103
	if testing.Short() {
		w, h = 41, 23
	}
	for _, kind := range testimg.Kinds {
		src := testimg.New(kind, w, h)
		for on, o := range opts {
			t.Run(kind+"-"+on, func(t *testing.T) {
				b := encode(t, src, o)
				in, out := filepath.Join(dir, kind+on+".webp"), filepath.Join(dir, kind+on+".pam")
				if err := os.WriteFile(in, b, 0o644); err != nil {
					t.Fatal(err)
				}
				if msg, err := exec.Command(bin, "-quiet", "-pam", in, "-o", out).CombinedOutput(); err != nil {
					t.Fatalf("dwebp: %v %s", err, msg)
				}
				p, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				ref, err := testimg.ParsePAM(p)
				if err != nil {
					t.Fatal(err)
				}
				if d := diff(decode(t, b), ref, false); d != 0 {
					t.Errorf("Decode differs from dwebp by %d", d)
				}
				if o != nil && o.Lossless {
					if d := diff(ref, src, !o.Exact); d != 0 {
						t.Errorf("dwebp of lossless differs from the source by %d", d)
					}
				}
			})
		}
	}
}

// TestImageTypes checks that every input type encodes to its color.NRGBAModel conversion.
func TestImageTypes(t *testing.T) {
	src := testimg.New("cutout-alpha", 64, 48)
	rgba := image.NewRGBA(src.Rect)
	draw.Draw(rgba, rgba.Rect, src, image.Point{}, draw.Src)
	ycc := image.NewYCbCr(image.Rect(0, 0, 64, 48), image.YCbCrSubsampleRatio420)
	for i := range ycc.Y {
		ycc.Y[i] = uint8(i * 7)
	}
	for i := range ycc.Cb {
		ycc.Cb[i], ycc.Cr[i] = uint8(i*3), uint8(255-i)
	}
	gray := image.NewGray(image.Rect(0, 0, 33, 17))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 20, 20), color.Palette{color.Transparent, color.NRGBA{200, 10, 10, 128}, color.White})
	for i := range pal.Pix {
		pal.Pix[i] = uint8(i % 3)
	}
	nrgba64 := image.NewNRGBA64(image.Rect(0, 0, 10, 10))
	for i := range nrgba64.Pix {
		nrgba64.Pix[i] = uint8(i * 13)
	}
	cases := map[string]image.Image{
		"NRGBA": src, "RGBA": rgba, "YCbCr": ycc, "Gray": gray, "Paletted": pal, "NRGBA64": nrgba64,
		"NRGBA sub": src.SubImage(image.Rect(5, 7, 50, 40)), "RGBA sub": rgba.SubImage(image.Rect(1, 2, 63, 47)),
		"YCbCr sub": ycc.SubImage(image.Rect(3, 3, 30, 20)),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			want := toNRGBA(m)
			got := decode(t, encode(t, m, &webp.Options{Lossless: true, Exact: true}))
			if d := diff(got, want, true); d != 0 {
				t.Errorf("max difference %d from color.NRGBAModel", d)
			}
		})
	}
}

func TestImageDecodeRegistered(t *testing.T) {
	for _, f := range []string{"testdata/photo-q75.webp", "testdata/icon-alpha-lossless.webp"} {
		b, _ := os.ReadFile(f)
		m, format, err := image.Decode(bytes.NewReader(b))
		if err != nil || format != "webp" {
			t.Fatalf("%s: image.Decode: %v %q", f, err, format)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || format != "webp" {
			t.Fatalf("%s: image.DecodeConfig: %v %q", f, err, format)
		}
		if cfg.Width != m.Bounds().Dx() || cfg.Height != m.Bounds().Dy() || cfg.ColorModel != m.ColorModel() {
			t.Errorf("%s: config %+v, image %v %v", f, cfg, m.Bounds(), m.ColorModel())
		}
	}
}

func TestDecodeConfigReadsLittle(t *testing.T) {
	b, _ := os.ReadFile("testdata/photo-q75.webp")
	r := &countingReader{r: bytes.NewReader(b)}
	cfg, err := webp.DecodeConfig(r)
	if err != nil || cfg.Width != 128 || cfg.Height != 96 || cfg.ColorModel != color.RGBAModel {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if r.n > 64 {
		t.Errorf("read %d bytes for the config", r.n)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// oneByteReader returns one byte per Read, to exercise the incremental reads.
type oneByteReader struct{ b []byte }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(o.b) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0], o.b = o.b[0], o.b[1:]
	return 1, nil
}

func TestDecodeReaders(t *testing.T) {
	b, _ := os.ReadFile("testdata/cutout-alpha-q75.webp")
	want := decode(t, b)
	m, err := webp.Decode(&oneByteReader{b})
	if err != nil {
		t.Fatal(err)
	}
	if d := diff(toNRGBA(m), want, false); d != 0 {
		t.Errorf("one-byte reader: difference %d", d)
	}
	cfg, err := webp.DecodeConfig(&oneByteReader{b})
	if err != nil || cfg.Width != 128 {
		t.Errorf("config through one-byte reader: %+v %v", cfg, err)
	}
	// Trailing bytes after the RIFF stream are not read.
	r := bytes.NewReader(append(append([]byte{}, b...), "trailing"...))
	if _, err := webp.Decode(r); err != nil {
		t.Fatal(err)
	}
	if r.Len() != len("trailing") {
		t.Errorf("left %d bytes unread, want %d", r.Len(), len("trailing"))
	}
}

// animated wraps a still WebP frame into a minimal animated WebP (VP8X + ANIM + ANMF).
func animated(t *testing.T) []byte {
	still, _ := os.ReadFile("testdata/flat-q75.webp")
	frame := still[12:] // the VP8 chunk
	le := func(v, n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(v >> (8 * i))
		}
		return b
	}
	chunk := func(id string, data []byte) []byte {
		b := append([]byte(id), le(len(data), 4)...)
		b = append(b, data...)
		if len(data)%2 == 1 {
			b = append(b, 0)
		}
		return b
	}
	vp8x := append([]byte{0x02, 0, 0, 0}, append(le(127, 3), le(95, 3)...)...)
	anim := append(le(0, 4), le(0, 2)...)
	anmf := append(append(le(0, 3), le(0, 3)...), append(append(le(127, 3), le(95, 3)...), append(le(100, 3), 0)...)...)
	body := append([]byte("WEBP"), chunk("VP8X", vp8x)...)
	body = append(body, chunk("ANIM", anim)...)
	body = append(body, chunk("ANMF", append(anmf, frame...))...)
	return append(append([]byte("RIFF"), le(len(body), 4)...), body...)
}

func TestErrors(t *testing.T) {
	good, _ := os.ReadFile("testdata/photo-q75.webp")
	ll, _ := os.ReadFile("testdata/photo-lossless.webp")
	corrupt := append([]byte{}, good...)
	for i := 40; i < len(corrupt); i++ {
		corrupt[i] ^= 0x5a
	}
	decodes := map[string]struct {
		in   []byte
		want error
	}{
		"empty":              {nil, webp.ErrTruncated},
		"short":              {[]byte("RIFF"), webp.ErrTruncated},
		"not webp":           {[]byte("\x89PNG\r\n\x1a\n0000000000000000000000000000000"), webp.ErrFormat},
		"truncated":          {good[:len(good)/2], webp.ErrTruncated},
		"truncated lossless": {ll[:len(ll)/2], webp.ErrTruncated},
		"corrupt":            {corrupt, webp.ErrFormat},
		"animated":           {animated(t), webp.ErrUnsupported},
	}
	for name, c := range decodes {
		t.Run("decode "+name, func(t *testing.T) {
			m, err := webp.Decode(bytes.NewReader(c.in))
			ok := errors.Is(err, c.want) || name == "corrupt" && errors.Is(err, webp.ErrTruncated)
			if !ok || m != nil {
				t.Errorf("got %v, %v; want %v", m, err, c.want)
			}
			var we *webp.Error
			if !errors.As(err, &we) || we.Op != "decode" {
				t.Errorf("error %v is not a decode *webp.Error", err)
			}
		})
	}
	if _, err := webp.DecodeConfig(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "))); !errors.Is(err, webp.ErrTruncated) && !errors.Is(err, webp.ErrFormat) {
		t.Errorf("config of a bare header: %v", err)
	}
	if _, err := webp.DecodeConfig(bytes.NewReader([]byte("GIF89a"))); !errors.Is(err, webp.ErrFormat) && !errors.Is(err, webp.ErrTruncated) {
		t.Errorf("config of GIF: %v", err)
	}
	cfg, err := webp.DecodeConfig(bytes.NewReader(animated(t)))
	if err != nil || cfg.Width != 128 {
		t.Errorf("config of animated: %+v %v", cfg, err)
	}

	small := testimg.New("flat", 8, 8)
	encodes := map[string]struct {
		m    image.Image
		o    *webp.Options
		want error
	}{
		"empty":     {image.NewNRGBA(image.Rect(0, 0, 0, 5)), nil, webp.ErrInvalidImage},
		"too wide":  {image.NewNRGBA(image.Rect(0, 0, webp.MaxDimension+1, 1)), nil, webp.ErrInvalidImage},
		"quality":   {small, &webp.Options{Quality: 101}, webp.ErrInvalidOptions},
		"quality-":  {small, &webp.Options{Quality: -1}, webp.ErrInvalidOptions},
		"method":    {small, &webp.Options{Quality: 50, Method: 7}, webp.ErrInvalidOptions},
		"method-":   {small, &webp.Options{Quality: 50, Method: -1}, webp.ErrInvalidOptions},
		"widest ok": {image.NewNRGBA(image.Rect(0, 0, webp.MaxDimension, 1)), &webp.Options{Lossless: true}, nil},
	}
	for name, c := range encodes {
		t.Run("encode "+name, func(t *testing.T) {
			err := webp.Encode(io.Discard, c.m, c.o)
			if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
				t.Errorf("got %v, want %v", err, c.want)
			}
		})
	}
	// Writer errors come back as they are; a panicking writer's panic is not swallowed.
	werr := errors.New("disk full")
	if err := webp.Encode(failWriter{werr}, small, nil); err != werr {
		t.Errorf("writer error: got %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Errorf("recovered %v, want the writer's panic", r)
			}
		}()
		_ = webp.Encode(panicWriter{}, small, nil)
	}()
	// The pool still works after all that.
	if d := diff(decode(t, encode(t, small, &webp.Options{Lossless: true})), small, false); d != 0 {
		t.Error("encode after errors differs")
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("boom") }

// TestConcurrent encodes and decodes from many goroutines at once (run it with -race): every
// result must equal the sequential one.
func TestConcurrent(t *testing.T) {
	type job struct {
		m    *image.NRGBA
		o    *webp.Options
		want []byte
	}
	var jobs []job
	for i, kind := range testimg.Kinds {
		m := testimg.New(kind, 60+i*7, 40+i*5)
		o := &webp.Options{Quality: float32(40 + 8*i), Method: i % 7, Lossless: i%3 == 0}
		jobs = append(jobs, job{m, o, encode(t, m, o)})
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for r := 0; r < 4; r++ {
				j := jobs[(g+r)%len(jobs)]
				var b bytes.Buffer
				if err := webp.Encode(&b, j.m, j.o); err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(b.Bytes(), j.want) {
					errs <- fmt.Errorf("goroutine %d: different output", g)
					return
				}
				if _, err := webp.Decode(bytes.NewReader(j.want)); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
