package attach_test

import (
	"runtime"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func waitUntil(t *testing.T, clk clock.Clock, timeout time.Duration, fn func() bool) {
	waitUntilDebug(t, clk, timeout, fn, nil)
}

func waitUntilDebug(t *testing.T, clk clock.Clock, timeout time.Duration, fn func() bool, debug func() string) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if fn() {
			return
		}
		ptytest.Advance(clk, 10*time.Millisecond)
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	if fn() {
		return
	}
	if debug != nil {
		t.Fatalf("condition not met before timeout: %s", debug())
	}
	t.Fatalf("condition not met before timeout")
}
