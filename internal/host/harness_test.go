package host_test

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func newHarness(t *testing.T, opts ...ptytest.HarnessOption) *ptytest.Harness {
	t.Helper()
	options := append([]ptytest.HarnessOption{ptytest.WithClock(clock.NewMock())}, opts...)
	return ptytest.New(t, options...)
}
