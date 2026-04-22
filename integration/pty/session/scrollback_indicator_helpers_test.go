//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func waitForStableTopRow(
	t *testing.T,
	sess *ptytest.PTYSession,
	timeout time.Duration,
	step time.Duration,
	stableSamples int,
	check func(row string) error,
) string {
	t.Helper()
	if stableSamples < 1 {
		stableSamples = 1
	}
	deadline := sess.Clock().Now().Add(timeout)
	prev := ""
	stable := 0
	lastRow := ""
	lastRaw := ""
	var lastErr error
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		row := sess.Screen().Row(0)
		lastRow = row
		lastRaw = raw
		if err := check(row); err != nil {
			lastErr = err
			prev = row
			stable = 0
			advanceTestClock(sess.Clock(), step)
			continue
		}
		if raw != "" {
			lastErr = fmt.Errorf("top row still receiving raw output")
			prev = row
			stable = 0
			advanceTestClock(sess.Clock(), step)
			continue
		}
		if row == prev {
			stable++
		} else {
			prev = row
			stable = 1
		}
		if stable >= stableSamples {
			return row
		}
		advanceTestClock(sess.Clock(), step)
	}
	t.Fatalf("timed out waiting for stable top row: row=%q raw=%q err=%v", lastRow, lastRaw, lastErr)
	return ""
}
