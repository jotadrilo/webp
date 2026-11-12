package webp_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests if any goroutine is left running: the package starts none.
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
