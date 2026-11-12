// Command mktestdata writes the committed test fixtures into testdata/: each synthetic image as
// PNG, WebP files made from it by libwebp's own cwebp, and what libwebp's dwebp decodes each
// of those to (testdata/golden/*.png). Run it through `make testdata` (which builds cwebp and
// dwebp from the pinned libwebp first).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jotadrilo/webp/internal/testimg"
)

// The cwebp variants: lossy at the defaults and at what thumbnails use, and lossless.
var variants = []struct {
	name string
	args []string
}{
	{"q75", []string{"-q", "75", "-m", "4"}},
	{"q80m0", []string{"-q", "80", "-m", "0"}},
	{"lossless", []string{"-lossless", "-q", "75", "-m", "4"}},
}

func main() {
	bin := flag.String("bin", ".cache/bin", "directory with cwebp and dwebp")
	out := flag.String("o", "testdata", "output directory")
	flag.Parse()
	must(os.MkdirAll(filepath.Join(*out, "golden"), 0o755))
	tmp, err := os.MkdirTemp("", "mktestdata")
	must(err)
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, kind := range testimg.Kinds {
		m := testimg.New(kind, 128, 96)
		var b bytes.Buffer
		must(png.Encode(&b, m))
		must(os.WriteFile(filepath.Join(*out, kind+".png"), b.Bytes(), 0o644))
		pam := filepath.Join(tmp, kind+".pam")
		must(os.WriteFile(pam, testimg.PAM(m), 0o644))
		for _, v := range variants {
			name := kind + "-" + v.name
			wp := filepath.Join(*out, name+".webp")
			run(filepath.Join(*bin, "cwebp"), append(append([]string{"-quiet"}, v.args...), pam, "-o", wp)...)
			dec := filepath.Join(tmp, name+".pam")
			run(filepath.Join(*bin, "dwebp"), "-quiet", "-pam", wp, "-o", dec)
			p, err := os.ReadFile(dec)
			must(err)
			g, err := testimg.ParsePAM(p)
			must(err)
			b.Reset()
			must(png.Encode(&b, g))
			must(os.WriteFile(filepath.Join(*out, "golden", name+".png"), b.Bytes(), 0o644))
		}
	}
	fmt.Println("wrote", *out)
}

func run(name string, args ...string) {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v: %v\n%s", name, args, err, out)
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
