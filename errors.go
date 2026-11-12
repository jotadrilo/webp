package webp

import (
	"errors"
	"fmt"
)

// Sentinel errors. Every error returned by this package wraps one of them (test with
// errors.Is) inside an *Error, except errors from the io.Reader or io.Writer, which are
// returned as they are.
var (
	// ErrFormat reports data that is not WebP or whose bitstream is corrupt.
	ErrFormat = errors.New("webp: invalid format")
	// ErrTruncated reports a WebP stream that ends early.
	ErrTruncated = errors.New("webp: truncated data")
	// ErrUnsupported reports a valid WebP feature this package does not decode (animation).
	ErrUnsupported = errors.New("webp: unsupported feature")
	// ErrInvalidImage reports an image that WebP cannot hold: empty, or wider or taller than
	// 16383 pixels.
	ErrInvalidImage = errors.New("webp: invalid image")
	// ErrInvalidOptions reports Options out of range.
	ErrInvalidOptions = errors.New("webp: invalid options")
	// ErrOutOfMemory reports that libwebp could not allocate memory.
	ErrOutOfMemory = errors.New("webp: out of memory")
	// ErrInternal reports a failure inside libwebp (it never happens with a correct build;
	// the instance that failed is discarded).
	ErrInternal = errors.New("webp: internal error")
)

// Error is the error type of this package: the operation, libwebp's status code, and the
// sentinel error it maps to.
type Error struct {
	Op   string // "encode", "decode" or "decode config"
	Code int    // libwebp's VP8StatusCode (decode) or WebPEncodingError (encode); 0 if none
	Err  error  // one of the sentinel errors above
}

func (e *Error) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("%v (%s, libwebp code %d)", e.Err, e.Op, e.Code)
	}
	return fmt.Sprintf("%v (%s)", e.Err, e.Op)
}

func (e *Error) Unwrap() error { return e.Err }

// libwebp's VP8StatusCode.
const (
	statusOK = iota
	statusOutOfMemory
	statusInvalidParam
	statusBitstreamError
	statusUnsupportedFeature
	statusSuspended
	statusUserAbort
	statusNotEnoughData
)

func decodeError(op string, st int32) error {
	var err error
	switch st {
	case statusOutOfMemory:
		err = ErrOutOfMemory
	case statusUnsupportedFeature:
		err = ErrUnsupported
	case statusNotEnoughData:
		err = ErrTruncated
	case statusInvalidParam:
		err = ErrInternal
	default:
		err = ErrFormat
	}
	return &Error{Op: op, Code: int(st), Err: err}
}

// libwebp's WebPEncodingError, plus the shim's own codes.
const (
	encOutOfMemory          = 1
	encBitstreamOutOfMemory = 2
	encBadDimension         = 5
	encBadWrite             = 8
	encFileTooBig           = 9
	encErrConfig            = 100
)

func encodeError(code int32) error {
	var err error
	switch code {
	case encOutOfMemory, encBitstreamOutOfMemory:
		err = ErrOutOfMemory
	case encBadDimension, encFileTooBig:
		err = ErrInvalidImage
	case encErrConfig:
		err = ErrInvalidOptions
	default:
		err = ErrInternal
	}
	return &Error{Op: "encode", Code: int(code), Err: err}
}
