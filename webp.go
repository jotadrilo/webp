package webp

import (
	"image"
	"io"

	"github.com/jotadrilo/webp/decode"
	"github.com/jotadrilo/webp/encode"
	"github.com/jotadrilo/webp/internal/werr"
)

// MaxDimension is the largest width or height a WebP image can have.
const MaxDimension = encode.MaxDimension

// Options are the encoding parameters. A nil *Options means DefaultOptions(). A non-nil
// Options is used as given (so start from DefaultOptions() when changing one field).
type Options = encode.Options

// DefaultOptions returns libwebp's defaults: lossy, Quality 75, Method 4.
func DefaultOptions() *Options { return encode.DefaultOptions() }

// Encode writes m to w in WebP format with libwebp, with options o (nil for the defaults).
// Alpha is kept exactly in both formats: lossy files carry it losslessly. *image.NRGBA,
// *image.RGBA and *image.Paletted are copied straight into libwebp's memory; opaque images of
// other types are converted there with image/draw, the rest through color.NRGBAModel.
func Encode(w io.Writer, m image.Image, o *Options) error { return encode.Encode(w, m, o) }

// Decode reads a WebP image from r and decodes it with libwebp, giving exactly the pixels of
// libwebp's dwebp (and browsers built on libwebp). It returns an *image.NRGBA when the image
// has alpha and an *image.RGBA (opaque, so the same bytes) when it has not.
//
// Animated WebP is not supported (ErrUnsupported). Like image/png, Decode allocates what the
// header asks for (up to 16383 x 16383 x 4 bytes); check DecodeConfig first for untrusted input
// whose size matters to you.
func Decode(r io.Reader) (image.Image, error) { return decode.Decode(r) }

// DecodeConfig returns the dimensions and color model of a WebP image (color.NRGBAModel with
// alpha, color.RGBAModel without), reading only its headers.
func DecodeConfig(r io.Reader) (image.Config, error) { return decode.DecodeConfig(r) }

// Sentinel errors. Every error returned by this package (and by webp/encode and webp/decode,
// which return the same values) wraps one of them inside an *Error, except errors from the
// io.Reader or io.Writer, which are returned as they are. Test them with errors.Is.
var (
	// ErrFormat reports data that is not WebP or whose bitstream is corrupt.
	ErrFormat = werr.ErrFormat
	// ErrTruncated reports a WebP stream that ends early.
	ErrTruncated = werr.ErrTruncated
	// ErrUnsupported reports a valid WebP feature this package does not decode (animation).
	ErrUnsupported = werr.ErrUnsupported
	// ErrInvalidImage reports an image that WebP cannot hold: empty, or wider or taller than
	// MaxDimension pixels.
	ErrInvalidImage = werr.ErrInvalidImage
	// ErrInvalidOptions reports Options out of range.
	ErrInvalidOptions = werr.ErrInvalidOptions
	// ErrOutOfMemory reports that libwebp could not allocate memory.
	ErrOutOfMemory = werr.ErrOutOfMemory
	// ErrInternal reports a failure inside libwebp (it never happens with a correct build; the
	// instance that failed is discarded).
	ErrInternal = werr.ErrInternal
)

// Error is the error type of this package: the operation, libwebp's status code, and the
// sentinel error it maps to.
type Error = werr.Error
