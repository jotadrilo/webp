// Command bench measures one WebP implementation per process, so that its allocations, GC and
// peak RSS are its own. It prints Go benchmark lines (for benchstat and cmd/report):
//
//	bench -prep <dir>                       write the inputs (thumbnails, WebP files, a 24 MP image)
//	bench -in <dir> -enc <name> -latency     one line per thumbnail: mean ns/op, p50/p95/p99,
//	                                         B/op, allocs/op, GC pause per op
//	bench -in <dir> -dec <name> -latency     the same for decoding the lossy and lossless inputs
//	bench -in <dir> -enc|-dec <name> -throughput  GOMAXPROCS concurrent jobs for -dur: images/s, peak RSS
//	bench -in <dir> -enc|-dec <name> -big    the 24 MP image once (after one warm-up): time, peak RSS
//	bench -in <dir> -enc <name> -quality <dwebp>  JSON: bytes and SSIM/PSNR scored through dwebp
//	bench -in <dir> -dec <name> -quality <dwebp>  JSON: difference from dwebp's pixels
//	bench -score <dir> -quality <dwebp>      JSON: score the .webp files C wrote into <dir>
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jotadrilo/webp/internal/testimg"
)

// The thumbnails: every synthetic kind at a 384 px long edge, like a thumbnail pipeline's
// output, plus a 24 MP photo for the large-image run.
var thumbSizes = map[string][2]int{
	"photo": {384, 256}, "flat": {384, 288}, "lineart": {288, 384}, "gradient": {384, 216},
	"cutout-alpha": {384, 384}, "icon-alpha": {256, 256}, "noise": {384, 256},
}

const bigName = "photo-24mp"

func main() {
	prep := flag.String("prep", "", "write the inputs into this directory and exit")
	in := flag.String("in", "work/in", "inputs directory")
	encName := flag.String("enc", "", "encoder to measure")
	decName := flag.String("dec", "", "decoder to measure")
	latency := flag.Bool("latency", false, "measure single-image latency")
	throughput := flag.Bool("throughput", false, "measure throughput with GOMAXPROCS concurrent workers")
	big := flag.Bool("big", false, "encode/decode the 24 MP image")
	quality := flag.String("quality", "", "path to dwebp: score outputs instead of timing")
	score := flag.String("score", "", "score the .webp files in this directory (C outputs)")
	minTime := flag.Duration("mintime", 300*time.Millisecond, "minimum timed duration per image")
	dur := flag.Duration("dur", time.Second, "throughput window")
	list := flag.Bool("list", false, "list encoders and decoders")
	flag.Parse()
	switch {
	case *list:
		for _, n := range sortedKeys(encoders) {
			fmt.Println("enc", n, encoders[n].group, encoders[n].multicore)
		}
		for _, n := range sortedKeys(decoders) {
			fmt.Println("dec", n, decoders[n].multicore)
		}
	case *prep != "":
		writeInputs(*prep)
	case *score != "":
		scoreDir(*in, *score, *quality)
	case *encName != "":
		e, ok := encoders[*encName]
		if !ok {
			fail("unknown encoder %q", *encName)
		}
		run("Encode", *encName, *in, encodeJobs(*in, e), *latency, *throughput, *big, *minTime, *dur, *quality,
			func() job { return encodeBig(*in, e) })
	case *decName != "":
		d, ok := decoders[*decName]
		if !ok {
			fail("unknown decoder %q", *decName)
		}
		run("Decode", *decName, *in, decodeJobs(*in, d), *latency, *throughput, *big, *minTime, *dur, *quality,
			func() job { return decodeBig(*in, d) })
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+f+"\n", a...)
	os.Exit(1)
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

// A job is one image's work: run does it once (and returns its output, for scoring).
type job struct {
	name string
	run  func() ([]byte, image.Image, error)
	src  *image.NRGBA // encode: the source; decode: dwebp's decoding of the input
	in   string       // decode: the input file
}

func readRaw(p string) *image.NRGBA {
	b, err := os.ReadFile(p)
	if err != nil {
		fail("%v (run bench -prep first)", err)
	}
	w, h := int(binary.LittleEndian.Uint32(b)), int(binary.LittleEndian.Uint32(b[4:]))
	return &image.NRGBA{Pix: b[8:], Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

func encodeJobs(in string, e encoder) []job {
	var jobs []job
	for _, k := range testimg.Kinds {
		m := readRaw(filepath.Join(in, k+".rgba"))
		var buf bytes.Buffer
		jobs = append(jobs, job{name: k, src: m, run: func() ([]byte, image.Image, error) {
			buf.Reset()
			err := e.enc(&buf, m)
			return buf.Bytes(), nil, err
		}})
	}
	return jobs
}

func encodeBig(in string, e encoder) job {
	m := readRaw(filepath.Join(in, bigName+".rgba"))
	return job{name: bigName, run: func() ([]byte, image.Image, error) {
		var c countWriter
		return nil, nil, e.enc(&c, m)
	}}
}

type countWriter struct{ n int }

func (c *countWriter) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

func decodeJobs(in string, d decoder) []job {
	var jobs []job
	for _, k := range testimg.Kinds {
		for _, v := range []string{"lossy", "lossless"} {
			p := filepath.Join(in, k+"-"+v+".webp")
			b, err := os.ReadFile(p)
			if err != nil {
				fail("%v", err)
			}
			jobs = append(jobs, job{name: k + "-" + v, in: p, run: func() ([]byte, image.Image, error) {
				m, err := d.dec(b)
				return nil, m, err
			}})
		}
	}
	return jobs
}

func decodeBig(in string, d decoder) job {
	b, err := os.ReadFile(filepath.Join(in, bigName+"-lossy.webp"))
	if err != nil {
		fail("%v", err)
	}
	return job{name: bigName, run: func() ([]byte, image.Image, error) {
		m, err := d.dec(b)
		return nil, m, err
	}}
}

func maxRSSMB() float64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	if runtime.GOOS == "darwin" {
		return float64(ru.Maxrss) / (1 << 20)
	}
	return float64(ru.Maxrss) / 1024
}

func run(op, name, in string, jobs []job, latency, throughput, big bool, minTime, dur time.Duration, dwebp string, bigJob func() job) {
	cpus := runtime.GOMAXPROCS(0)
	prefix := fmt.Sprintf("Benchmark%s/impl=%s", op, name)
	switch {
	case dwebp != "":
		if op == "Encode" {
			scoreEncoder(name, jobs, dwebp)
		} else {
			scoreDecoder(name, jobs, dwebp)
		}
	case latency:
		for _, j := range jobs {
			measureLatency(fmt.Sprintf("%s/img=%s/cpus=%d", prefix, j.name, cpus), j, minTime)
		}
	case throughput:
		for _, j := range jobs { // warm up every image once
			if _, _, err := j.run(); err != nil {
				fail("%s %s: %v", name, j.name, err)
			}
		}
		var count atomic.Int64
		var stop atomic.Bool
		var wg sync.WaitGroup
		t0 := time.Now()
		for w := 0; w < cpus; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				// Each worker needs its own buffers: rebuild the jobs per worker.
				var mine []job
				if op == "Encode" {
					mine = encodeJobs(in, encoders[name])
				} else {
					mine = decodeJobs(in, decoders[name])
				}
				for k := w; !stop.Load(); k++ {
					if _, _, err := mine[k%len(mine)].run(); err != nil {
						fail("%v", err)
					}
					count.Add(1)
				}
			}(w)
		}
		time.Sleep(dur)
		stop.Store(true)
		wg.Wait()
		wall := time.Since(t0)
		n := count.Load()
		fmt.Printf("%sThroughput/impl=%s/cpus=%d\t%d\t%.0f ns/op\t%.2f img/s\t%.1f peak-RSS-MB\n",
			"Benchmark"+op, name, cpus, n, float64(wall.Nanoseconds())/float64(n), float64(n)/wall.Seconds(), maxRSSMB())
	case big:
		j := bigJob()
		base := maxRSSMB()
		if _, _, err := j.run(); err != nil {
			fail("%s big: %v", name, err)
		}
		runtime.GC()
		t0 := time.Now()
		if _, _, err := j.run(); err != nil {
			fail("%s big: %v", name, err)
		}
		d := time.Since(t0)
		fmt.Printf("%s24MP/impl=%s/cpus=%d\t1\t%d ns/op\t%.1f peak-RSS-MB\t%.1f base-RSS-MB\n", "Benchmark"+op, name, cpus, d.Nanoseconds(), maxRSSMB(), base)
	}
}

func measureLatency(label string, j job, minTime time.Duration) {
	for i := 0; i < 2; i++ {
		if _, _, err := j.run(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
			return
		}
	}
	runtime.GC()
	var a, z runtime.MemStats
	runtime.ReadMemStats(&a)
	var ds []time.Duration
	start := time.Now()
	for len(ds) < 5 || (time.Since(start) < minTime && len(ds) < 1000) {
		t := time.Now()
		_, _, _ = j.run()
		ds = append(ds, time.Since(t))
	}
	total := time.Since(start)
	runtime.ReadMemStats(&z)
	n := len(ds)
	slices.Sort(ds)
	pct := func(p float64) int64 { return ds[min(n-1, int(math.Ceil(p*float64(n)))-1)].Nanoseconds() }
	fmt.Printf("%s\t%d\t%.0f ns/op\t%d p50-ns\t%d p95-ns\t%d p99-ns\t%.0f B/op\t%.1f allocs/op\t%.0f gc-pause-ns/op\n",
		label, n, float64(total.Nanoseconds())/float64(n), pct(0.50), pct(0.95), pct(0.99),
		float64(z.TotalAlloc-a.TotalAlloc)/float64(n), float64(z.Mallocs-a.Mallocs)/float64(n),
		float64(z.PauseTotalNs-a.PauseTotalNs)/float64(n))
}

// ---- inputs ----

func writeInputs(dir string) {
	must(os.MkdirAll(dir, 0o755))
	cwebp := os.Getenv("CWEBP")
	if cwebp == "" {
		fail("CWEBP must point to libwebp's cwebp")
	}
	write := func(name string, m *image.NRGBA) {
		w, h := m.Rect.Dx(), m.Rect.Dy()
		b := make([]byte, 8, 8+len(m.Pix))
		binary.LittleEndian.PutUint32(b, uint32(w))
		binary.LittleEndian.PutUint32(b[4:], uint32(h))
		b = append(b, m.Pix...)
		must(os.WriteFile(filepath.Join(dir, name+".rgba"), b, 0o644))
		pam := filepath.Join(dir, name+".pam")
		must(os.WriteFile(pam, testimg.PAM(m), 0o644))
		// Decoder inputs, made by libwebp's own cwebp: lossy (q80 m4) and lossless.
		for v, args := range map[string][]string{"lossy": {"-q", "80", "-m", "4"}, "lossless": {"-lossless"}} {
			if name == bigName && v == "lossless" {
				continue
			}
			out := filepath.Join(dir, name+"-"+v+".webp")
			if msg, err := exec.Command(cwebp, append(append([]string{"-quiet"}, args...), pam, "-o", out)...).CombinedOutput(); err != nil {
				fail("cwebp: %v %s", err, msg)
			}
		}
		must(os.Remove(pam))
	}
	for _, k := range testimg.Kinds {
		sz := thumbSizes[k]
		write(k, testimg.New(k, sz[0], sz[1]))
		var b bytes.Buffer
		m := testimg.New(k, sz[0], sz[1])
		must(png.Encode(&b, m))
		must(os.WriteFile(filepath.Join(dir, k+".png"), b.Bytes(), 0o644))
	}
	write(bigName, testimg.New("photo", 6000, 4000))
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

// ---- quality ----

type encScore struct {
	Impl  string  `json:"impl"`
	Image string  `json:"image"`
	Bytes int     `json:"bytes"`
	SSIM  float64 `json:"ssim"`
	PSNR  float64 `json:"psnr"`
	// AlphaMaxDiff is the largest alpha difference (0: alpha kept exactly).
	AlphaMaxDiff int    `json:"alpha_max_diff"`
	Err          string `json:"err,omitempty"`
}

type decScore struct {
	Impl  string `json:"impl"`
	Image string `json:"image"`
	// Difference from libwebp's dwebp: the largest per-channel difference, the mean absolute
	// difference, and the PSNR (99 when identical).
	MaxDiff  int     `json:"max_diff"`
	MeanDiff float64 `json:"mean_diff"`
	PSNR     float64 `json:"psnr"`
	Err      string  `json:"err,omitempty"`
}

func dwebpDecode(dwebp string, data []byte) (*image.NRGBA, error) {
	dir, err := os.MkdirTemp("", "bench-dwebp")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in, out := filepath.Join(dir, "in.webp"), filepath.Join(dir, "out.pam")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return nil, err
	}
	if msg, err := exec.Command(dwebp, "-quiet", "-pam", in, "-o", out).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("dwebp rejects it: %v %s", err, strings.TrimSpace(string(msg)))
	}
	p, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return testimg.ParsePAM(p)
}

func scoreEncoder(name string, jobs []job, dwebp string) {
	for _, j := range jobs {
		out, _, err := j.run()
		s := encScore{Impl: name, Image: j.name, Bytes: len(out)}
		if err != nil {
			s.Err = err.Error()
		} else if dec, err := dwebpDecode(dwebp, out); err != nil {
			s.Err = err.Error()
		} else {
			s.SSIM, s.PSNR, s.AlphaMaxDiff = compare(j.src, dec)
		}
		must(json.NewEncoder(os.Stdout).Encode(s))
	}
}

// scoreDir scores C's outputs (<dir>/<image>.webp) like scoreEncoder.
func scoreDir(in, dir, dwebp string) {
	name := filepath.Base(dir)
	for _, k := range testimg.Kinds {
		src := readRaw(filepath.Join(in, k+".rgba"))
		out, err := os.ReadFile(filepath.Join(dir, k+".webp"))
		s := encScore{Impl: name, Image: k, Bytes: len(out)}
		if err != nil {
			s.Err = err.Error()
		} else if dec, err := dwebpDecode(dwebp, out); err != nil {
			s.Err = err.Error()
		} else {
			s.SSIM, s.PSNR, s.AlphaMaxDiff = compare(src, dec)
		}
		must(json.NewEncoder(os.Stdout).Encode(s))
	}
}

func scoreDecoder(name string, jobs []job, dwebp string) {
	for _, j := range jobs {
		s := decScore{Impl: name, Image: j.name}
		data, err := os.ReadFile(j.in)
		must(err)
		ref, err := dwebpDecode(dwebp, data)
		must(err)
		_, m, err := j.run()
		if err != nil {
			s.Err = err.Error()
		} else {
			s.MaxDiff, s.MeanDiff, s.PSNR = pixelDiff(ref, toNRGBA(m))
		}
		must(json.NewEncoder(os.Stdout).Encode(s))
	}
}

// toNRGBA converts m with Go's standard color conversion, as a user of the decoder gets it.
func toNRGBA(m image.Image) *image.NRGBA {
	if n, ok := m.(*image.NRGBA); ok {
		return n
	}
	b := m.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			n.Set(x, y, m.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return n
}

func pixelDiff(a, b *image.NRGBA) (maxDiff int, mean, psnr float64) {
	if a.Rect.Size() != b.Rect.Size() {
		return 255, 255, 0
	}
	var se, sum float64
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		maxDiff = max(maxDiff, d)
		sum += float64(d)
		se += float64(d * d)
	}
	n := float64(len(a.Pix))
	return maxDiff, sum / n, psnrOf(se / n)
}

func psnrOf(mse float64) float64 {
	if mse == 0 {
		return 99
	}
	return 10 * math.Log10(255*255/mse)
}

// compare scores dec against src: SSIM of luma and PSNR of RGB, both composited over white
// (what a viewer sees), and the largest alpha difference.
func compare(src, dec *image.NRGBA) (ssimV, psnrV float64, alphaMax int) {
	if src.Rect.Size() != dec.Rect.Size() {
		return 0, 0, 255
	}
	sy, srgb, sa := planes(src)
	dy, drgb, da := planes(dec)
	var se float64
	for i := range srgb {
		d := srgb[i] - drgb[i]
		se += d * d
	}
	for i := range sa {
		alphaMax = max(alphaMax, int(math.Abs(sa[i]-da[i])))
	}
	return ssim(sy, dy, src.Rect.Dx(), src.Rect.Dy()), psnrOf(se / float64(len(srgb))), alphaMax
}

func planes(m *image.NRGBA) (y, rgb, a []float64) {
	w, h := m.Rect.Dx(), m.Rect.Dy()
	y, rgb, a = make([]float64, w*h), make([]float64, 3*w*h), make([]float64, w*h)
	for k := 0; k < w*h; k++ {
		p := m.Pix[4*k : 4*k+4]
		al := float64(p[3]) / 255
		r := float64(p[0])*al + 255*(1-al)
		g := float64(p[1])*al + 255*(1-al)
		b := float64(p[2])*al + 255*(1-al)
		rgb[3*k], rgb[3*k+1], rgb[3*k+2] = r, g, b
		y[k] = 0.299*r + 0.587*g + 0.114*b
		a[k] = float64(p[3])
	}
	return
}

// ssim is the mean SSIM over an 11x11 Gaussian window (sigma 1.5), Wang et al. 2004.
func ssim(x, y []float64, w, h int) float64 {
	const r = 5
	var k [2*r + 1]float64
	var s float64
	for i := -r; i <= r; i++ {
		k[i+r] = math.Exp(-float64(i*i) / (2 * 1.5 * 1.5))
		s += k[i+r]
	}
	for i := range k {
		k[i] /= s
	}
	blur := func(p []float64) []float64 {
		tmp, out := make([]float64, len(p)), make([]float64, len(p))
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				var v float64
				for d := -r; d <= r; d++ {
					v += k[d+r] * p[j*w+min(max(i+d, 0), w-1)]
				}
				tmp[j*w+i] = v
			}
		}
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				var v float64
				for d := -r; d <= r; d++ {
					v += k[d+r] * tmp[min(max(j+d, 0), h-1)*w+i]
				}
				out[j*w+i] = v
			}
		}
		return out
	}
	n := len(x)
	xx, yy, xy := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range x {
		xx[i], yy[i], xy[i] = x[i]*x[i], y[i]*y[i], x[i]*y[i]
	}
	mx, my, sxx, syy, sxy := blur(x), blur(y), blur(xx), blur(yy), blur(xy)
	c1, c2 := (0.01*255)*(0.01*255), (0.03*255)*(0.03*255)
	var sum float64
	for i := 0; i < n; i++ {
		vx, vy, cv := sxx[i]-mx[i]*mx[i], syy[i]-my[i]*my[i], sxy[i]-mx[i]*my[i]
		sum += ((2*mx[i]*my[i] + c1) * (2*cv + c2)) / ((mx[i]*mx[i] + my[i]*my[i] + c1) * (vx + vy + c2))
	}
	return sum / float64(n)
}
