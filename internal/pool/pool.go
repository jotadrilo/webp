// Package pool keeps libwebp module instances for reuse. A module is not safe for concurrent
// use, so each call takes one and gives it back. Creating a module costs ~0.1 ms and the memory
// it starts with; reusing one costs nothing, and its memory (grown to fit the largest image it
// has handled) is reused as it is.
package pool

import (
	"runtime"
	"sync/atomic"
	"time"
)

// MaxMemory bounds the linear memory of a module kept for reuse. A module that grew beyond it
// (a large image) is dropped after its call, so that its memory goes back to the Go heap
// instead of staying pinned in the pool.
const MaxMemory = 32 << 20

// IdleTrim is how long the pool must go unused before garbage collections start releasing its
// instances, one per collection (the runtime collects at least every two minutes), so that
// memory grown during a burst of concurrent calls goes back to the Go heap once the package is
// idle, without a background goroutine.
const IdleTrim = 10 * time.Second

// Pool holds at most GOMAXPROCS (at creation) idle instances of T.
type Pool[T any] struct {
	idle    chan T
	newT    func() T
	memSize func(T) int
	lastUse atomic.Int64 // nanoseconds since start
	start   time.Time
}

// New returns a pool that creates instances with newT and measures their memory with memSize.
func New[T any](newT func() T, memSize func(T) int) *Pool[T] {
	p := &Pool[T]{idle: make(chan T, runtime.GOMAXPROCS(0)), newT: newT, memSize: memSize, start: time.Now()}
	p.armTrim()
	return p
}

func (p *Pool[T]) armTrim() {
	s := new([16]byte) // 16 bytes: not a tiny allocation, so its cleanup runs
	runtime.AddCleanup(s, func(*Pool[T]) {
		if p.Idle() >= IdleTrim {
			select {
			case <-p.idle:
			default:
			}
		}
		p.armTrim()
	}, p)
}

// Get returns an idle instance, or a new one.
func (p *Pool[T]) Get() T {
	p.lastUse.Store(int64(time.Since(p.start)))
	select {
	case t := <-p.idle:
		return t
	default:
		return p.newT()
	}
}

// Put gives t back for reuse, unless its memory grew beyond MaxMemory or the pool is full.
func (p *Pool[T]) Put(t T) {
	if p.memSize(t) > MaxMemory {
		return
	}
	select {
	case p.idle <- t:
	default:
	}
}

// Len returns the number of idle instances.
func (p *Pool[T]) Len() int { return len(p.idle) }

// Cap returns the most idle instances the pool keeps.
func (p *Pool[T]) Cap() int { return cap(p.idle) }

// Idle returns the time since the last Get.
func (p *Pool[T]) Idle() time.Duration {
	return time.Since(p.start) - time.Duration(p.lastUse.Load())
}

// SetLastUse is for tests: it pretends the last Get was d ago.
func (p *Pool[T]) SetLastUse(d time.Duration) { p.lastUse.Store(int64(time.Since(p.start) - d)) }
