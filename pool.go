package webp

import (
	"io"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/jotadrilo/webp/internal/gen"
)

// An instance is one libwebp module: its own linear memory and C heap. A module is not safe for
// concurrent use, so each call takes one from the pool and gives it back when done. Creating a
// module costs ~0.1 ms and the memory it starts with; reusing one costs nothing, and its memory
// (grown to fit the largest image it has handled) is reused as it is.
type instance struct {
	m    *gen.Module
	w    io.Writer // the destination of the encode in progress
	werr error     // the first error w returned
	wpan any       // a panic raised by w, re-raised to the caller
}

// maxPooledMemory bounds the linear memory of a module kept for reuse. A module that grew
// beyond it (a large image) is dropped after its call, so that its memory goes back to the Go
// heap instead of staying pinned in the pool.
const maxPooledMemory = 32 << 20

var pool = make(chan *instance, runtime.GOMAXPROCS(0))

// lastUse is when an instance was last taken (nanoseconds since start). At each garbage
// collection, if the package has been idle for idleTrim, the pool releases one instance, so that
// memory grown during a burst of concurrent calls goes back to the Go heap once the package is
// idle (the runtime collects at least every two minutes), without a background goroutine. Use
// in between keeps every instance warm.
var lastUse atomic.Int64

const idleTrim = 10 * time.Second

var start = time.Now()

func init() { armTrim() }

func armTrim() {
	s := new([16]byte) // 16 bytes: not a tiny allocation, so its cleanup runs
	runtime.AddCleanup(s, func(struct{}) {
		if time.Since(start)-time.Duration(lastUse.Load()) >= idleTrim {
			select {
			case <-pool:
			default:
			}
		}
		armTrim()
	}, struct{}{})
}

func getInstance() *instance {
	lastUse.Store(int64(time.Since(start)))
	select {
	case i := <-pool:
		return i
	default:
	}
	i := new(instance)
	i.m = gen.New(i, i)
	i.m.X_initialize()
	return i
}

func putInstance(i *instance) {
	i.w, i.werr, i.wpan = nil, nil, nil
	if len(i.m.Mem()) > maxPooledMemory {
		return
	}
	select {
	case pool <- i:
	default:
	}
}

// Xwrite is the module's host_write import: it hands encoded bytes straight from the module's
// memory to the caller's io.Writer (which, per io.Writer, must not retain them).
func (i *instance) Xwrite(p, n int32) int32 {
	if i.werr != nil {
		return 0
	}
	defer func() {
		if r := recover(); r != nil {
			i.wpan = r
			panic(r)
		}
	}()
	mem := i.m.Mem()
	if _, err := i.w.Write(mem[uint32(p) : uint32(p)+uint32(n)]); err != nil {
		i.werr = err
		return 0
	}
	return 1
}

// Xrandom_get is the WASI import wasi-libc uses to seed its stack-protector canary. The
// module's memory is a bounds-checked Go slice, so the canary protects nothing here; a fixed
// value keeps every instance deterministic.
func (i *instance) Xrandom_get(p, n int32) int32 {
	clear(i.m.Mem()[uint32(p) : uint32(p)+uint32(n)])
	return 0
}

// call runs f on a pooled instance. A panic inside the module (a trap) discards the instance
// and becomes ErrInternal, so bad input can never crash the caller.
func call(op string, f func(*instance) error) (err error) {
	i := getInstance()
	defer func() {
		if r := recover(); r != nil {
			if i.wpan != nil {
				panic(i.wpan) // the caller's io.Writer panicked: not ours to hide
			}
			err = &Error{Op: op, Err: ErrInternal}
			return
		}
		putInstance(i)
	}()
	return f(i)
}
