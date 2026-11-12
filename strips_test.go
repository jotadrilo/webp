package webp_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jotadrilo/webp"
	"github.com/jotadrilo/webp/internal/testimg"
)

// cwebp returns the path to libwebp's cwebp: $CWEBP, or the one `make dwebp` builds.
func cwebp(t testing.TB) string {
	if p := os.Getenv("CWEBP"); p != "" {
		return p
	}
	if p, err := filepath.Abs(".cache/bin/cwebp"); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatal("cwebp not found: run make dwebp")
	}
	t.Skip("cwebp not found: run make dwebp")
	return ""
}

// TestEncodeStripsMatchCwebp: Encode imports pixels in strips of rows; the output must still be
// cwebp's, byte for byte, whatever the height and wherever the transparent pixels are.
func TestEncodeStripsMatchCwebp(t *testing.T) {
	bin := cwebp(t)
	dir := t.TempDir()
	type tc struct {
		name string
		w, h int
		// alphaRows makes the rows [from, to) translucent (the rest opaque).
		from, to int
	}
	cases := []tc{
		{"opaque-1", 7, 1, 0, 0}, {"opaque-31", 40, 31, 0, 0}, {"opaque-33", 41, 33, 0, 0}, {"opaque-97", 50, 97, 0, 0},
		{"alpha-last-row", 40, 65, 64, 65}, {"alpha-first-row", 40, 65, 0, 1}, {"alpha-mid-strip", 33, 97, 40, 41},
		{"alpha-strip-edge", 33, 96, 31, 33}, {"alpha-odd-row", 20, 70, 63, 64}, {"alpha-all", 30, 45, 0, 45},
	}
	if testing.Short() {
		cases = cases[:5]
	}
	opts := map[string]struct {
		o    *webp.Options
		args []string
	}{
		"q75m4":    {&webp.Options{Quality: 75, Method: 4}, []string{"-q", "75", "-m", "4"}},
		"q80m0":    {&webp.Options{Quality: 80, Method: 0}, []string{"-q", "80", "-m", "0"}},
		"lossless": {&webp.Options{Lossless: true, Quality: 30, Method: 2}, []string{"-lossless", "-q", "30", "-m", "2"}},
		"exact":    {&webp.Options{Quality: 75, Method: 4, Exact: true}, []string{"-q", "75", "-m", "4", "-exact"}},
	}
	for _, c := range cases {
		m := testimg.New("photo", c.w, c.h)
		for y := c.from; y < c.to; y++ {
			for x := 0; x < c.w; x++ {
				m.Pix[m.PixOffset(x, y)+3] = uint8((x * 37) % 256)
			}
		}
		pam := filepath.Join(dir, c.name+".pam")
		if err := os.WriteFile(pam, testimg.PAM(m), 0o644); err != nil {
			t.Fatal(err)
		}
		for on, o := range opts {
			t.Run(fmt.Sprintf("%s-%s", c.name, on), func(t *testing.T) {
				out := filepath.Join(dir, c.name+on+".webp")
				if msg, err := exec.Command(bin, append(append([]string{"-quiet"}, o.args...), pam, "-o", out)...).CombinedOutput(); err != nil {
					t.Fatalf("cwebp: %v %s", err, msg)
				}
				want, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				var got bytes.Buffer
				if err := webp.Encode(&got, m, o.o); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got.Bytes(), want) {
					t.Errorf("differs from cwebp: %d bytes, cwebp %d", got.Len(), len(want))
				}
			})
		}
	}
}
