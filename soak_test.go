package webp_test

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/jotadrilo/webp"
	"github.com/jotadrilo/webp/internal/pool"
	"github.com/jotadrilo/webp/internal/testimg"
)

type soakSample struct {
	images int
	// The heap with the pools as they are, then after releasing every idle pooled instance
	// (what would remain if nothing leaked: close to nothing), and the process's current and
	// peak RSS after returning freed memory to the OS.
	heapInuseMB, heapObjsK, drainedHeapMB, drainedObjsK, rssMB, maxRSSMB float64
	numGC                                                                uint32
}

// TestSoak encodes and decodes many images (mixed sizes, kinds, alpha, options, with
// concurrent bursts) and checks that nothing leaks: at every sample, after the idle pooled
// instances are released (the pool keeps at most GOMAXPROCS of them, whose sizes depend on
// which images they last served), the heap and the process's RSS must be back within a small
// margin of where they were after the warm-up. WEBP_SOAK sets the
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
		s := soakSample{images: images, heapInuseMB: float64(ms.HeapInuse) / (1 << 20), heapObjsK: float64(ms.HeapObjects) / 1000}
		pool.ReleaseIdle()
		runtime.GC()
		debug.FreeOSMemory()
		runtime.ReadMemStats(&ms)
		s.drainedHeapMB, s.drainedObjsK = float64(ms.HeapInuse)/(1<<20), float64(ms.HeapObjects)/1000
		s.rssMB, s.maxRSSMB, s.numGC = currentRSSMB(), maxRSSMB(), ms.NumGC
		return s
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
	csv.WriteString("images,heap_inuse_mb,heap_objects_k,drained_heap_mb,drained_objects_k,rss_mb,max_rss_mb,num_gc\n")
	for _, s := range samples {
		fmt.Fprintf(&csv, "%d,%.2f,%.1f,%.2f,%.1f,%.1f,%.1f,%d\n", s.images, s.heapInuseMB, s.heapObjsK,
			s.drainedHeapMB, s.drainedObjsK, s.rssMB, s.maxRSSMB, s.numGC)
	}
	t.Logf("soak samples:\n%s", csv.String())
	if p := os.Getenv("WEBP_SOAK_CSV"); p != "" {
		if err := os.WriteFile(p, csv.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Compare the end with the sample after the first fifth (after the first burst).
	ref, last := samples[len(samples)/5], samples[len(samples)-1]
	if last.drainedHeapMB > ref.drainedHeapMB*1.10+1 {
		t.Errorf("heap grew: %.2f MB in use after %d images (pools drained), %.2f MB after %d",
			last.drainedHeapMB, last.images, ref.drainedHeapMB, ref.images)
	}
	if last.drainedObjsK > ref.drainedObjsK*1.10+1 {
		t.Errorf("heap objects grew: %.1fk after %d images, %.1fk after %d", last.drainedObjsK, last.images, ref.drainedObjsK, ref.images)
	}
	if ref.rssMB > 0 && last.rssMB > ref.rssMB*1.25+16 {
		t.Errorf("RSS grew: %.0f MB after %d images (pools drained), %.0f MB after %d", last.rssMB, last.images, ref.rssMB, ref.images)
	}
}

// currentRSSMB returns the process's resident set size now, from ps (0 if unavailable).
func currentRSSMB() float64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return kb / 1024
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
