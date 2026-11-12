package encode

import (
	"io"

	"github.com/jotadrilo/webp/internal/gen/enc"
	"github.com/jotadrilo/webp/internal/pool"
	"github.com/jotadrilo/webp/internal/werr"
)

// An instance is one libwebp encoder module, with the destination of the encode in progress.
type instance struct {
	m    *enc.Module
	w    io.Writer // the destination of the encode in progress
	werr error     // the first error w returned
	wpan any       // a panic raised by w, re-raised to the caller
}

var instances = pool.New(func() *instance {
	i := new(instance)
	i.m = enc.New(i, i)
	i.m.X_initialize()
	return i
}, func(i *instance) int { return len(i.m.Mem()) })

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
func call(f func(*instance) error) (err error) {
	i := instances.Get()
	defer func() {
		if r := recover(); r != nil {
			if i.wpan != nil {
				panic(i.wpan) // the caller's io.Writer panicked: not ours to hide
			}
			err = &werr.Error{Op: "encode", Err: werr.ErrInternal}
			return
		}
		i.w, i.werr, i.wpan = nil, nil, nil
		instances.Put(i)
	}()
	return f(i)
}
