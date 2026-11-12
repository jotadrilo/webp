package webp_test

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"testing"

	"github.com/jotadrilo/webp"
	"github.com/jotadrilo/webp/internal/testimg"
)

type soakSample struct {
	images                        int
	heapInuseMB, heapObjsK, rssMB float64
	numGC                         uint32
}

// TestSoak encodes and decodes many images (mixed sizes, kinds, alpha, options, with
// concurrent bursts) and checks that memory plateaus: the heap after the run is within a small
// margin of the heap after the warm-up, and the peak RSS stops growing. WEBP_SOAK sets the
// count (default 2000; the benchmark runs 50000), WEBP_SOAK_CSV writes the samples, and
// WEBP_SOAK_PROFILE (a directory) writes heap profiles at 1000 and 10000 images.
func TestSoak(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("soak: not with -short or -race")
	}
	n := 2000
	if s := os.Getenv("WEBP_SOAK"); s != "" {
		var err error
		if n, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	every := max(n/50, 100)
	if n >= 10000 {
		every = 1000
	}
	profDir := os.Getenv("WEBP_SOAK_PROFILE")

	// A fixed set of sources: sizes from 1 px to 640 px, every kind (two with alpha).
	type src struct {
		m *image.NRGBA
		o *webp.Options
	}
	var srcs []src
	sizes := [][2]int{{1, 1}, {17, 9}, {64, 64}, {160, 120}, {384, 256}, {256, 384}, {640, 480}}
	opts := []*webp.Options{{Quality: 80}, {Quality: 80, Method: 4}, {Quality: 60, Method: 2}, {Lossless: true, Quality: 20, Method: 1}}
	for i, kind := range testimg.Kinds {
		for j, sz := range sizes {
			o := opts[(i+j)%len(opts)]
			if o.Lossless && sz[0]*sz[1] > 160*120 {
				o = opts[0] // lossless is 50x slower: keep it to small images
			}
			srcs = append(srcs, src{testimg.New(kind, sz[0], sz[1]), o})
		}
	}
	work := func(k int, buf *bytes.Buffer) {
		s := srcs[k%len(srcs)]
		buf.Reset()
		if err := webp.Encode(buf, s.m, s.o); err != nil {
			t.Error(err)
			return
		}
		if _, err := webp.Decode(bytes.NewReader(buf.Bytes())); err != nil {
			t.Error(err)
		}
	}
	sample := func(images int) soakSample {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return soakSample{images, float64(ms.HeapInuse) / (1 << 20), float64(ms.HeapObjects) / 1000, maxRSSMB(), ms.NumGC}
	}
	var samples []soakSample
	var buf bytes.Buffer
	for k := 1; k <= n; k++ {
		if k%5000 == 0 || k == n/10 {
			// A concurrent burst: more goroutines than the pool holds.
			var wg sync.WaitGroup
			for g := 0; g < 4*runtime.GOMAXPROCS(0); g++ {
				wg.Add(1)
				go func(g int) {
					defer wg.Done()
					var b bytes.Buffer
					for r := 0; r < 8; r++ {
						work(k+g*31+r, &b)
					}
				}(g)
			}
			wg.Wait()
		}
		work(k, &buf)
		if k%every == 0 {
			samples = append(samples, sample(k))
		}
		if profDir != "" && (k == 1000 || k == 10000) {
			runtime.GC()
			f, err := os.Create(filepath.Join(profDir, fmt.Sprintf("heap-%d.pprof", k)))
			if err != nil {
				t.Fatal(err)
			}
			if err := pprof.WriteHeapProfile(f); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}
	var csv bytes.Buffer
	csv.WriteString("images,heap_inuse_mb,heap_objects_k,max_rss_mb,num_gc\n")
	for _, s := range samples {
		fmt.Fprintf(&csv, "%d,%.2f,%.1f,%.1f,%d\n", s.images, s.heapInuseMB, s.heapObjsK, s.rssMB, s.numGC)
	}
	t.Logf("soak samples:\n%s", csv.String())
	if p := os.Getenv("WEBP_SOAK_CSV"); p != "" {
		if err := os.WriteFile(p, csv.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Compare the end with the sample after the first fifth (after the first burst, so the
	// pool holds its share of instances by then).
	ref, last := samples[len(samples)/5], samples[len(samples)-1]
	if last.heapInuseMB > ref.heapInuseMB*1.10+2 {
		t.Errorf("heap grew: %.1f MB in use after %d images, %.1f MB after %d", last.heapInuseMB, last.images, ref.heapInuseMB, ref.images)
	}
	if last.heapObjsK > ref.heapObjsK*1.10+1 {
		t.Errorf("heap objects grew: %.1fk after %d images, %.1fk after %d", last.heapObjsK, last.images, ref.heapObjsK, ref.images)
	}
	if last.rssMB > ref.rssMB*1.15+8 {
		t.Errorf("peak RSS grew: %.0f MB after %d images, %.0f MB after %d", last.rssMB, last.images, ref.rssMB, ref.images)
	}
}

func maxRSSMB() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return float64(ru.Maxrss) / (1 << 20)
	}
	return float64(ru.Maxrss) / 1024
}
