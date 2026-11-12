// Package testimg synthesizes the deterministic test and benchmark images: no third-party
// content, so testdata/ carries no license questions. Each image stresses a different part of
// an encoder: photographic texture, flat color, thin lines, smooth gradients, alpha.
package testimg

import (
	"image"
	"math"
	"math/rand/v2"
)

// Kinds lists the image kinds, in a stable order.
var Kinds = []string{"photo", "flat", "lineart", "gradient", "cutout-alpha", "icon-alpha", "noise"}

// New returns image kind at w x h. Opaque kinds have every alpha at 255.
func New(kind string, w, h int) *image.NRGBA {
	switch kind {
	case "photo":
		return photo(w, h, 1)
	case "flat":
		return flat(w, h)
	case "lineart":
		return lineart(w, h)
	case "gradient":
		return gradient(w, h)
	case "cutout-alpha":
		return softAlpha(photo(w, h, 7))
	case "icon-alpha":
		return icon(w, h)
	case "noise":
		return noise(w, h)
	}
	panic("testimg: unknown kind " + kind)
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

func set(m *image.NRGBA, x, y int, r, g, b, a float64) {
	i := m.PixOffset(x, y)
	m.Pix[i] = uint8(clamp01(r)*255 + 0.5)
	m.Pix[i+1] = uint8(clamp01(g)*255 + 0.5)
	m.Pix[i+2] = uint8(clamp01(b)*255 + 0.5)
	m.Pix[i+3] = uint8(clamp01(a)*255 + 0.5)
}

// photo: multi-octave value noise for structure, smooth color fields, a few hard edges and
// per-pixel grain, so it neither compresses trivially nor is pure noise.
func photo(w, h int, seed uint64) *image.NRGBA {
	r := rand.New(rand.NewPCG(seed, 2))
	scales := []float64{0.5, 0.18, 0.06, 0.02}
	type grid struct {
		v      [][]float64
		gw, gh int
		s      float64
	}
	grids := make([]grid, len(scales))
	for i, f := range scales {
		s := math.Max(2, f*float64(max(w, h)))
		gw, gh := int(float64(w)/s)+2, int(float64(h)/s)+2
		v := make([][]float64, gh)
		for y := range v {
			v[y] = make([]float64, gw)
			for x := range v[y] {
				v[y][x] = r.Float64()
			}
		}
		grids[i] = grid{v, gw, gh, s}
	}
	sample := func(g grid, x, y int) float64 {
		fx, fy := float64(x)/g.s, float64(y)/g.s
		x0, y0 := int(fx), int(fy)
		tx, ty := fx-float64(x0), fy-float64(y0)
		tx, ty = tx*tx*(3-2*tx), ty*ty*(3-2*ty)
		a := g.v[y0][x0]*(1-tx) + g.v[y0][x0+1]*tx
		b := g.v[y0+1][x0]*(1-tx) + g.v[y0+1][x0+1]*tx
		return a*(1-ty) + b*ty
	}
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n, amp := 0.0, 0.55
			for _, g := range grids {
				n += amp * sample(g, x, y)
				amp *= 0.5
			}
			gx, gy := float64(x)/float64(w), float64(y)/float64(h)
			edge := 0.0
			if (x*5/w+y*3/h)%3 == 0 {
				edge = 0.12
			}
			grain := (r.Float64() - 0.5) * 0.05
			set(m, x, y, 0.2+0.65*n*gx+edge+grain, 0.15+0.75*n*(1-gy)+grain,
				0.25+0.55*n*gy+0.2*gx+edge+grain, 1)
		}
	}
	return m
}

// flat: a flat illustration: antialiased discs and bars in a small palette.
func flat(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	pal := [][3]float64{{0.96, 0.93, 0.85}, {0.91, 0.42, 0.33}, {0.20, 0.47, 0.62}, {0.98, 0.78, 0.30}, {0.30, 0.62, 0.44}}
	s := float64(min(w, h))
	type disc struct {
		cx, cy, r float64
		c         int
	}
	discs := []disc{{0.3, 0.35, 0.22, 1}, {0.7, 0.6, 0.28, 2}, {0.45, 0.75, 0.12, 3}, {0.8, 0.2, 0.1, 4}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := pal[0]
			if (y*8/h)%2 == 1 && x > w/10 && x < w/10+w/12 {
				c = pal[4]
			}
			for _, d := range discs {
				dist := math.Hypot(float64(x)+0.5-d.cx*float64(w), float64(y)+0.5-d.cy*float64(h)) - d.r*s
				if k := clamp01(0.5 - dist); k > 0 {
					p := pal[d.c]
					c = [3]float64{c[0]*(1-k) + p[0]*k, c[1]*(1-k) + p[1]*k, c[2]*(1-k) + p[2]*k}
				}
			}
			set(m, x, y, c[0], c[1], c[2], 1)
		}
	}
	return m
}

// lineart: thin antialiased dark strokes (circles and diagonals) on white.
func lineart(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			d := math.Inf(1)
			for i := 1; i <= 6; i++ {
				rr := float64(i) * float64(min(w, h)) / 14
				d = math.Min(d, math.Abs(math.Hypot(fx-float64(w)*0.4, fy-float64(h)*0.5)-rr))
			}
			for i := 0; i < 5; i++ {
				off := float64(i) * float64(w) / 5
				d = math.Min(d, math.Abs(fx-fy*0.6-off)/math.Hypot(1, 0.6))
			}
			ink := clamp01(1.2 - d)
			v := 1 - 0.88*ink
			set(m, x, y, v, v, v*0.97+0.03, 1)
		}
	}
	return m
}

// gradient: smooth two-axis gradients (banding shows here).
func gradient(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			gx, gy := float64(x)/float64(w-1+1), float64(y)/float64(h)
			set(m, x, y, 0.1+0.8*gx, 0.2+0.6*gy, 0.9-0.7*gx*gy, 1)
		}
	}
	return m
}

// softAlpha cuts an ellipse out of src with a soft edge; outside it the alpha is 0 but the color
// is kept (so Exact makes a difference).
func softAlpha(m *image.NRGBA) *image.NRGBA {
	b := m.Bounds()
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	rx, ry := cx*0.8, cy*0.9
	edge := math.Max(2, float64(b.Dx())/10)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dx, dy := (float64(x)-cx)/rx, (float64(y)-cy)/ry
			d := (math.Sqrt(dx*dx+dy*dy) - 1) * rx
			m.Pix[m.PixOffset(x, y)+3] = uint8(clamp01(1-d/edge)*255 + 0.5)
		}
	}
	return m
}

// icon: flat shapes with antialiased alpha on a fully transparent background.
func icon(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	s := float64(min(w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)+0.5-float64(w)/2, float64(y)+0.5-float64(h)/2
			// rounded square
			q := math.Max(math.Abs(fx), math.Abs(fy)) - 0.36*s
			a := clamp01(0.5 - q)
			c := [3]float64{0.15, 0.55, 0.85}
			if d := math.Hypot(fx, fy) - 0.18*s; d < 0.5 {
				k := clamp01(0.5 - d)
				c = [3]float64{c[0]*(1-k) + k, c[1]*(1-k) + 0.95*k, c[2]*(1-k) + 0.9*k}
			}
			set(m, x, y, c[0], c[1], c[2], a)
		}
	}
	return m
}

// noise: uniform random RGB: the worst case for every encoder.
func noise(w, h int) *image.NRGBA {
	r := rand.New(rand.NewPCG(3, 4))
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		if i%4 == 3 {
			m.Pix[i] = 255
		} else {
			m.Pix[i] = uint8(r.UintN(256))
		}
	}
	return m
}

// Opaque reports whether every pixel of m is opaque.
func Opaque(m *image.NRGBA) bool {
	for i := 3; i < len(m.Pix); i += 4 {
		if m.Pix[i] != 255 {
			return false
		}
	}
	return true
}
