package pool_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jotadrilo/webp/internal/pool"
)

type inst struct{ mem int }

// TestBoundedAndShrinks: the pool never holds more than its capacity, keeps instances while in
// use, and releases them one per GC after IdleTrim without use.
func TestBoundedAndShrinks(t *testing.T) {
	created := 0
	var mu sync.Mutex
	p := pool.New(func() *inst { mu.Lock(); created++; mu.Unlock(); return &inst{} }, func(i *inst) int { return i.mem })
	n := 4 * p.Cap()
	got := make([]*inst, n)
	for k := range got {
		got[k] = p.Get()
	}
	for _, i := range got {
		p.Put(i)
	}
	if p.Len() != p.Cap() {
		t.Fatalf("pool holds %d after a burst of %d, want its capacity %d", p.Len(), n, p.Cap())
	}
	p.Put(&inst{mem: pool.MaxMemory + 1})
	if p.Len() != p.Cap() {
		t.Fatal("an oversized instance was kept")
	}
	_ = p.Get()
	runtime.GC()
	runtime.GC()
	if p.Len() != p.Cap()-1 {
		t.Fatalf("recent use: GC released instances (%d left)", p.Len())
	}
	p.SetLastUse(pool.IdleTrim)
	deadline := time.Now().Add(10 * time.Second)
	for p.Len() > 0 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	if p.Len() != 0 {
		t.Errorf("%d instances still pooled after idle GC cycles", p.Len())
	}
	if created != n {
		t.Errorf("created %d instances for %d concurrent Gets", created, n)
	}
}
