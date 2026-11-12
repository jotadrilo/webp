package webp

import (
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/jotadrilo/webp/internal/testimg"
)

// TestPoolTrim checks that idle instances are released over a few GC cycles.
func TestPoolTrim(t *testing.T) {
	m := testimg.New("flat", 32, 32)
	done := make(chan struct{})
	n := min(4, cap(pool))
	for g := 0; g < n; g++ {
		go func() { _ = Encode(io.Discard, m, nil); done <- struct{}{} }()
	}
	for g := 0; g < n; g++ {
		<-done
	}
	if len(pool) == 0 {
		t.Fatal("no pooled instance after encoding")
	}
	// Recent use: GC cycles keep the instances.
	runtime.GC()
	runtime.GC()
	if len(pool) == 0 {
		t.Fatal("instances released while in use")
	}
	// Pretend the last use was long ago.
	lastUse.Store(int64(time.Since(start) - idleTrim))
	deadline := time.Now().Add(10 * time.Second)
	for len(pool) > 0 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	if len(pool) != 0 {
		t.Errorf("%d instances still pooled after idle GC cycles", len(pool))
	}
}
