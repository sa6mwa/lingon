package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func newHarness(t *testing.T, opts ...ptytest.HarnessOption) *ptytest.Harness {
	t.Helper()
	tracePath := filepath.Join(t.TempDir(), "lingon-trace.jsonl")
	if keepTrace := os.Getenv("LINGON_KEEP_TRACE_PATH"); keepTrace != "" {
		tracePath = keepTrace
	}
	options := append([]ptytest.HarnessOption{
		ptytest.WithClock(clock.NewMock()),
		ptytest.WithTracePath(tracePath),
	}, opts...)
	return ptytest.New(t, options...)
}
