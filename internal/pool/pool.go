// Package pool keeps libwebp module instances for reuse. A module is not safe for concurrent
// use, so each call takes one and gives it back. Creating a module costs ~0.1 ms and the memory
// it starts with; reusing one costs nothing, and its memory (grown to fit the largest image it
// has handled) is reused as it is.
package pool

import (
	"runtime"
	"sync"
	"time"
)

// MaxMemory bounds the linear memory of a module kept for reuse. A module that grew beyond it
// (a large image) is dropped after its call, so that its memory goes back to the Go heap
// instead of staying pinned in the pool.
const MaxMemory = 32 << 20

// IdleTrim is how long an idle instance stays pooled. At each garbage collection (the runtime
// collects at least every two minutes) the instances idle for longer are released, so memory
// grown during a burst of concurrent calls goes back to the Go heap, without a background
// goroutine.
const IdleTrim = 10 * time.Second

type entry[T any] struct {
	t    T
	idle time.Duration // since the pool's start, when it was put back
}

// Pool holds at most GOMAXPROCS (at creation) idle instances of T, as a stack: the most recently
// used instance is reused first, so sequential calls keep reusing one warm instance (whose
// memory has grown to fit them) while the rest go cold and are released.
type Pool[T any] struct {
	mu      sync.Mutex
	stack   []entry[T] // stack[0] is the coldest
	max     int
	newT    func() T
	memSize func(T) int
	start   time.Time
}

// New returns a pool that creates instances with newT and measures their memory with memSize.
func New[T any](newT func() T, memSize func(T) int) *Pool[T] {
	p := &Pool[T]{max: runtime.GOMAXPROCS(0), newT: newT, memSize: memSize, start: time.Now()}
	p.armTrim()
	return p
}

func (p *Pool[T]) armTrim() {
	s := new([16]byte) // 16 bytes: not a tiny allocation, so its cleanup runs
	runtime.AddCleanup(s, func(p *Pool[T]) {
		p.trim(IdleTrim)
		p.armTrim()
	}, p)
}

// trim releases the instances idle for at least d.
func (p *Pool[T]) trim(d time.Duration) {
	now := time.Since(p.start)
	p.mu.Lock()
	k := 0
	for k < len(p.stack) && now-p.stack[k].idle >= d {
		k++
	}
	if k > 0 {
		n := copy(p.stack, p.stack[k:])
		clear(p.stack[n:])
		p.stack = p.stack[:n]
	}
	p.mu.Unlock()
}

// Get returns the most recently used idle instance, or a new one.
func (p *Pool[T]) Get() T {
	p.mu.Lock()
	if n := len(p.stack); n > 0 {
		t := p.stack[n-1].t
		p.stack[n-1] = entry[T]{}
		p.stack = p.stack[:n-1]
		p.mu.Unlock()
		return t
	}
	p.mu.Unlock()
	return p.newT()
}

// Put gives t back for reuse, unless its memory grew beyond MaxMemory or the pool is full.
func (p *Pool[T]) Put(t T) {
	if p.memSize(t) > MaxMemory {
		return
	}
	now := time.Since(p.start)
	p.mu.Lock()
	if len(p.stack) < p.max {
		p.stack = append(p.stack, entry[T]{t, now})
	}
	p.mu.Unlock()
}

// Len returns the number of idle instances.
func (p *Pool[T]) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.stack)
}

// Cap returns the most idle instances the pool keeps.
func (p *Pool[T]) Cap() int { return p.max }

// Age is for tests: it pretends every idle instance was put back d ago.
func (p *Pool[T]) Age(d time.Duration) {
	p.mu.Lock()
	for i := range p.stack {
		p.stack[i].idle -= d
	}
	p.mu.Unlock()
}
