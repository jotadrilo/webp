// Command report turns a benchmark run directory (see run.sh) into Markdown tables: medians
// across rounds of each implementation's per-image times, ratios to native C libwebp measured
// in the same run, throughput and its scaling with CPUs, allocations, GC pauses, peak RSS,
// output size and quality, and decoders' agreement with libwebp's dwebp.
//
//	report <run dir>
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sample is one benchmark line: name keys and the metrics by unit.
type sample struct {
	op, impl, img string
	cpus          int
	m             map[string]float64
}

var lineRE = regexp.MustCompile(`^Benchmark(\w+)/impl=([^/\s]+)(?:/img=([^/\s]+))?/cpus=(\d+)\s+\d+\s+(.*)$`)

func readSamples(path string) []sample {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		g := lineRE.FindStringSubmatch(sc.Text())
		if g == nil {
			continue
		}
		cpus, _ := strconv.Atoi(g[4])
		s := sample{op: g[1], impl: g[2], img: g[3], cpus: cpus, m: map[string]float64{}}
		f := strings.Fields(g[5])
		for i := 0; i+1 < len(f); i += 2 {
			v, err := strconv.ParseFloat(f[i], 64)
			if err == nil {
				s.m[f[i+1]] = v
			}
		}
		out = append(out, s)
	}
	return out
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

type key struct {
	op, impl, img string
	cpus          int
}

// medians groups samples by (op, impl, img, cpus) and takes each metric's median over rounds.
func medians(ss []sample) map[key]map[string]float64 {
	acc := map[key]map[string][]float64{}
	for _, s := range ss {
		k := key{s.op, s.impl, s.img, s.cpus}
		if acc[k] == nil {
			acc[k] = map[string][]float64{}
		}
		for u, v := range s.m {
			acc[k][u] = append(acc[k][u], v)
		}
	}
	out := map[key]map[string]float64{}
	for k, us := range acc {
		out[k] = map[string]float64{}
		for u, v := range us {
			out[k][u] = median(v)
		}
	}
	return out
}

// perImpl averages a metric over the images of (op, impl, cpus); NaN if any image is missing.
func perImpl(med map[key]map[string]float64, op, impl string, cpus int, unit string, imgs []string) float64 {
	var sum float64
	for _, img := range imgs {
		v, ok := med[key{op, impl, img, cpus}][unit]
		if !ok {
			return math.NaN()
		}
		sum += v
	}
	return sum / float64(len(imgs))
}

type encScore struct {
	Impl, Image  string
	Bytes        int
	SSIM, PSNR   float64
	AlphaMaxDiff int `json:"alpha_max_diff"`
	Err          string
}

type decScore struct {
	Impl, Image string
	MaxDiff     int     `json:"max_diff"`
	MeanDiff    float64 `json:"mean_diff"`
	PSNR        float64
	Err         string
}

func readJSONL[T any](path string) []T {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []T
	dec := json.NewDecoder(f)
	for {
		var v T
		if dec.Decode(&v) != nil {
			return out
		}
		out = append(out, v)
	}
}

func ms(ns float64) string {
	if math.IsNaN(ns) {
		return "–"
	}
	switch v := ns / 1e6; {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func ratio(a, b float64) string {
	if math.IsNaN(a) || math.IsNaN(b) || b == 0 {
		return "–"
	}
	return fmt.Sprintf("%.2f×", a/b)
}

func num(v float64, f string) string {
	if math.IsNaN(v) {
		return "–"
	}
	return fmt.Sprintf(f, v)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: report <run dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	lat := medians(readSamples(filepath.Join(dir, "latency.txt")))
	thr := medians(readSamples(filepath.Join(dir, "throughput.txt")))
	big := medians(readSamples(filepath.Join(dir, "big.txt")))
	encQ := readJSONL[encScore](filepath.Join(dir, "quality-enc.jsonl"))
	decQ := readJSONL[decScore](filepath.Join(dir, "quality-dec.jsonl"))

	var cpuList []int
	seen := map[int]bool{}
	impls := map[string]map[string]bool{} // op -> impls
	imgs := map[string]map[string]bool{}
	for k := range lat {
		if !seen[k.cpus] {
			seen[k.cpus] = true
			cpuList = append(cpuList, k.cpus)
		}
		if impls[k.op] == nil {
			impls[k.op], imgs[k.op] = map[string]bool{}, map[string]bool{}
		}
		impls[k.op][k.impl] = true
		imgs[k.op][k.img] = true
	}
	sort.Ints(cpuList)
	keys := func(m map[string]bool) []string {
		var s []string
		for k := range m {
			s = append(s, k)
		}
		sort.Strings(s)
		return s
	}
	if b, err := os.ReadFile(filepath.Join(dir, "env.txt")); err == nil {
		fmt.Printf("Environment:\n\n```\n%s```\n\n", b)
	}
	cpuHdr := func(f string) string {
		var s []string
		for _, c := range cpuList {
			s = append(s, fmt.Sprintf(f, c))
		}
		return strings.Join(s, " | ")
	}
	align := func(n int) string { return strings.Repeat("---:|", n) }

	// ---- encoders ----
	groups := []struct{ name, c, title string }{
		{"lossy-m0", "c-libwebp-m0", "Lossy, quality 80, method 0 (thumbnail setting)"},
		{"lossy-m4", "c-libwebp-m4", "Lossy, quality 80, method 4 (libwebp's default method)"},
		{"lossless", "c-libwebp-lossless", "Lossless, default effort"},
	}
	q := map[string][]encScore{}
	for _, s := range encQ {
		q[s.Impl] = append(q[s.Impl], s)
	}
	eImgs := keys(imgs["Encode"])
	for _, g := range groups {
		var members []string
		for _, impl := range keys(impls["Encode"]) {
			if impl == g.c || strings.HasSuffix(impl, "-"+strings.TrimPrefix(g.name, "lossy-")) {
				members = append(members, impl)
			}
		}
		if len(members) == 0 {
			continue
		}
		sort.SliceStable(members, func(i, j int) bool {
			return perImpl(lat, "Encode", members[i], cpuList[0], "ns/op", eImgs) < perImpl(lat, "Encode", members[j], cpuList[0], "ns/op", eImgs)
		})
		fmt.Printf("### Encode: %s\n\nMean over the %d thumbnails of each one's median time (ms); × C is the ratio to native libwebp at the same CPU count, in the same run.\n\n", g.title, len(eImgs))
		fmt.Printf("| implementation | %s | %s | p99 ms (1 CPU) | alloc KB/op | GC pause µs/op | output KB | mean SSIM | mean PSNR dB | alpha exact | errors |\n",
			cpuHdr("ms, %d CPU"), cpuHdr("× C, %d CPU"))
		fmt.Printf("|---|%s%s%s\n", align(2*len(cpuList)), align(4), align(5))
		for _, impl := range members {
			var t, r []string
			for _, c := range cpuList {
				v := perImpl(lat, "Encode", impl, c, "ns/op", eImgs)
				t = append(t, ms(v))
				r = append(r, ratio(v, perImpl(lat, "Encode", g.c, c, "ns/op", eImgs)))
			}
			p99 := perImpl(lat, "Encode", impl, cpuList[0], "p99-ns", eImgs)
			alloc := perImpl(lat, "Encode", impl, cpuList[0], "B/op", eImgs) / 1024
			gc := perImpl(lat, "Encode", impl, cpuList[0], "gc-pause-ns/op", eImgs) / 1000
			var bytes int
			var ssim, psnr float64
			n, errs, alphaOK := 0, 0, true
			for _, s := range q[impl] {
				bytes += s.Bytes
				if s.Err != "" {
					errs++
					continue
				}
				n++
				ssim += s.SSIM
				psnr += s.PSNR
				alphaOK = alphaOK && s.AlphaMaxDiff == 0
			}
			sq, pq, ae := "–", "–", "–"
			if n > 0 {
				sq, pq = fmt.Sprintf("%.4f", ssim/float64(n)), fmt.Sprintf("%.2f", psnr/float64(n))
				ae = map[bool]string{true: "yes", false: "no"}[alphaOK]
			}
			fmt.Printf("| %s | %s | %s | %s | %s | %s | %.1f | %s | %s | %s | %d |\n", impl, strings.Join(t, " | "), strings.Join(r, " | "),
				ms(p99), num(alloc, "%.0f"), num(gc, "%.0f"), float64(bytes)/1024, sq, pq, ae, errs)
		}
		fmt.Println()
	}

	// ---- decoders ----
	dImgs := keys(imgs["Decode"])
	if len(dImgs) > 0 {
		dq := map[string][]decScore{}
		for _, s := range decQ {
			dq[s.Impl] = append(dq[s.Impl], s)
		}
		dm := keys(impls["Decode"])
		sort.SliceStable(dm, func(i, j int) bool {
			return perImpl(lat, "Decode", dm[i], cpuList[0], "ns/op", dImgs) < perImpl(lat, "Decode", dm[j], cpuList[0], "ns/op", dImgs)
		})
		fmt.Printf("### Decode\n\nThe %d inputs are the thumbnails encoded by libwebp's cwebp, lossy (q80 m4) and lossless. Agreement with dwebp: the largest per-channel difference from libwebp's own decoding over all inputs, and the mean PSNR against it on the lossy ones (99: identical).\n\n", len(dImgs))
		fmt.Printf("| implementation | %s | %s | p99 ms (1 CPU) | alloc KB/op | GC pause µs/op | max diff vs dwebp | lossy PSNR vs dwebp | errors |\n", cpuHdr("ms, %d CPU"), cpuHdr("× C, %d CPU"))
		fmt.Printf("|---|%s%s%s\n", align(2*len(cpuList)), align(3), align(3))
		for _, impl := range dm {
			var t, r []string
			for _, c := range cpuList {
				v := perImpl(lat, "Decode", impl, c, "ns/op", dImgs)
				t = append(t, ms(v))
				r = append(r, ratio(v, perImpl(lat, "Decode", "c-libwebp", c, "ns/op", dImgs)))
			}
			maxd, errs := 0, 0
			var psnr float64
			n := 0
			for _, s := range dq[impl] {
				if s.Err != "" {
					errs++
					continue
				}
				maxd = max(maxd, s.MaxDiff)
				if strings.HasSuffix(s.Image, "-lossy") {
					psnr += s.PSNR
					n++
				}
			}
			md, ps := "–", "–"
			if impl == "c-libwebp" {
				md, ps = "0 (it is dwebp's decoder)", "99"
			} else if len(dq[impl]) > 0 {
				md = strconv.Itoa(maxd)
				if n > 0 {
					ps = fmt.Sprintf("%.2f", psnr/float64(n))
				}
			}
			fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s | %d |\n", impl, strings.Join(t, " | "), strings.Join(r, " | "),
				ms(perImpl(lat, "Decode", impl, cpuList[0], "p99-ns", dImgs)),
				num(perImpl(lat, "Decode", impl, cpuList[0], "B/op", dImgs)/1024, "%.0f"),
				num(perImpl(lat, "Decode", impl, cpuList[0], "gc-pause-ns/op", dImgs)/1000, "%.0f"), md, ps, errs)
		}
		fmt.Println()
	}

	// ---- throughput ----
	for _, op := range []string{"EncodeThroughput", "DecodeThroughput"} {
		var names []string
		got := map[string]bool{}
		for k := range thr {
			if k.op == op && !got[k.impl] {
				got[k.impl] = true
				names = append(names, k.impl)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		c1 := cpuList[0]
		sort.SliceStable(names, func(i, j int) bool {
			return thr[key{op, names[i], "", c1}]["img/s"] > thr[key{op, names[j], "", c1}]["img/s"]
		})
		fmt.Printf("### %s: %s, N concurrent workers on N CPUs\n\nImages per second over the thumbnails (one worker per CPU); scaling is the N-CPU throughput over N times the 1-CPU one.\n\n",
			strings.TrimSuffix(op, "Throughput"), "throughput")
		var sc []string
		for _, c := range cpuList[1:] {
			sc = append(sc, fmt.Sprintf("scaling %d CPU", c))
		}
		last := cpuList[len(cpuList)-1]
		fmt.Printf("| implementation | %s | %s | × C, %d CPU | peak RSS MB, %d CPU |\n", cpuHdr("img/s, %d CPU"), strings.Join(sc, " | "), last, last)
		fmt.Printf("|---|%s%s\n", align(len(cpuList)+len(sc)), align(2))
		cName := "c-libwebp"
		for _, impl := range names {
			var t, s []string
			base := thr[key{op, impl, "", c1}]["img/s"]
			for _, c := range cpuList {
				t = append(t, num(thr[key{op, impl, "", c}]["img/s"], "%.0f"))
			}
			for _, c := range cpuList[1:] {
				s = append(s, num(thr[key{op, impl, "", c}]["img/s"]/(float64(c)/float64(c1)*base)*100, "%.0f %%"))
			}
			cref := cName
			if op == "EncodeThroughput" {
				for _, g := range groups {
					if strings.HasSuffix(impl, "-"+strings.TrimPrefix(g.name, "lossy-")) {
						cref = g.c
					}
				}
			}
			cv := thr[key{op, cref, "", last}]["img/s"]
			fmt.Printf("| %s | %s | %s | %s | %s |\n", impl, strings.Join(t, " | "), strings.Join(s, " | "),
				num(cv/thr[key{op, impl, "", last}]["img/s"], "%.2f×"), num(thr[key{op, impl, "", last}]["peak-RSS-MB"], "%.0f"))
		}
		fmt.Println()
	}

	// ---- 24 MP ----
	if len(big) > 0 {
		fmt.Printf("### One 24 MP image (6000 × 4000), 1 CPU\n\nTime of one encode or decode after a warm-up, and the peak RSS of the process above what it held before (the 96 MB RGBA source of an encode is in the baseline).\n\n")
		fmt.Printf("| operation | implementation | time ms | peak RSS above baseline MB |\n|---|---|---:|---:|\n")
		var ks []key
		for k := range big {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool {
			if ks[i].op != ks[j].op {
				return ks[i].op > ks[j].op
			}
			return big[ks[i]]["ns/op"] < big[ks[j]]["ns/op"]
		})
		for _, k := range ks {
			fmt.Printf("| %s | %s | %s | %.0f |\n", strings.TrimSuffix(k.op, "24MP"), k.impl, ms(big[k]["ns/op"]), big[k]["peak-RSS-MB"]-big[k]["base-RSS-MB"])
		}
		fmt.Println()
	}
}
