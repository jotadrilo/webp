package testimg

import (
	"bytes"
	"fmt"
	"image"
	"strings"
)

// PAM encodes m as a PAM file (RGB_ALPHA), the format libwebp's cwebp reads and dwebp writes.
func PAM(m *image.NRGBA) []byte {
	w, h := m.Bounds().Dx(), m.Bounds().Dy()
	var b bytes.Buffer
	fmt.Fprintf(&b, "P7\nWIDTH %d\nHEIGHT %d\nDEPTH 4\nMAXVAL 255\nTUPLTYPE RGB_ALPHA\nENDHDR\n", w, h)
	for y := 0; y < h; y++ {
		o := m.PixOffset(m.Rect.Min.X, m.Rect.Min.Y+y)
		b.Write(m.Pix[o : o+w*4])
	}
	return b.Bytes()
}

// ParsePAM decodes an RGB_ALPHA PAM file such as `dwebp -pam` writes.
func ParsePAM(p []byte) (*image.NRGBA, error) {
	end := bytes.Index(p, []byte("ENDHDR\n"))
	if end < 0 || !bytes.HasPrefix(p, []byte("P7\n")) {
		return nil, fmt.Errorf("testimg: not a PAM file")
	}
	var w, h, depth int
	for _, line := range strings.Split(string(p[:end]), "\n") {
		fmt.Sscanf(line, "WIDTH %d", &w)
		fmt.Sscanf(line, "HEIGHT %d", &h)
		fmt.Sscanf(line, "DEPTH %d", &depth)
	}
	px := p[end+7:]
	if depth != 4 || w <= 0 || h <= 0 || len(px) < w*h*4 {
		return nil, fmt.Errorf("testimg: unsupported PAM %dx%d depth %d", w, h, depth)
	}
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(m.Pix, px)
	return m, nil
}
