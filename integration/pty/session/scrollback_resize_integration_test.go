//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

var scrollbackPercentSuffix = regexp.MustCompile(`\[(\d{1,3})%\]\s*$`)

func TestHostResizePreservesScrollbackHistory(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-resize-scrollback-preserve",
		SessionName: "host-resize-scrollback-preserve",
		Shell:       shell,
		Cols:        90,
		Rows:        20,
	})
	t.Cleanup(host.Cancel)

	host.Send("i=1; while [ $i -le 140 ]; do printf 'LINE-%03d\\n' $i; i=$((i+1)); done\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("LINE-140") {
			return fmt.Errorf("waiting for generated scroll content")
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
		if _, ok := scrollbackPercent(row); !ok {
			return fmt.Errorf("waiting for scrollback indicator")
		}
		return nil
	})

	reachTop := func(maxSteps int) bool {
		for i := 0; i < maxSteps; i++ {
			if host.Screen().Contains("LINE-001") {
				return true
			}
			host.SendBytes([]byte{0x1b, '[', '5', '~'}) // PgUp
			advanceTestClock(h.Clock(), 60*time.Millisecond)
		}
		return host.Screen().Contains("LINE-001")
	}
	if !reachTop(24) {
		t.Fatalf("expected LINE-001 in scrollback before resize, got:\n%s", host.Screen().String())
	}

	host.Resize(64, 14)
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	host.SendBytes([]byte{0x1b, '[', 'F'}) // End
	advanceTestClock(h.Clock(), 60*time.Millisecond)
	host.SendBytes([]byte{0x1b, '[', 'H'}) // Home
	advanceTestClock(h.Clock(), 60*time.Millisecond)

	if !reachTop(24) {
		t.Fatalf("expected LINE-001 to remain reachable after resize; history appears erased:\n%s", host.Screen().String())
	}
}

func TestHostScrollbackResizeRepaintsIndicatorWithoutInput(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-resize-scrollback-indicator",
		SessionName: "host-resize-scrollback-indicator",
		Shell:       shell,
		Cols:        90,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	host.Send("i=1; while [ $i -le 220 ]; do printf 'ROW-%03d\\n' $i; i=$((i+1)); done\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("ROW-220") {
			return fmt.Errorf("waiting for generated rows")
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
		if _, ok := scrollbackPercent(row); !ok {
			return fmt.Errorf("waiting for scrollback indicator")
		}
		return nil
	})

	for i := 0; i < 8; i++ {
		host.SendBytes([]byte{0x1b, '[', '5', '~'}) // PgUp
		advanceTestClock(h.Clock(), 70*time.Millisecond)
	}

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		pct, ok := scrollbackPercent(host.Screen().Row(0))
		if !ok {
			return fmt.Errorf("scrollback indicator missing before resize")
		}
		if pct <= 0 || pct >= 100 {
			return fmt.Errorf("expected mid-scroll percent before resize, got %d", pct)
		}
		return nil
	})

	host.Resize(60, 24)
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	waitForStableTopRow(t, host, 1200*time.Millisecond, 50*time.Millisecond, 3, func(row string) error {
		_, ok := scrollbackPercent(row)
		if !ok {
			return fmt.Errorf("expected right-aligned scrollback indicator to repaint after width resize, got row=%q", row)
		}
		return nil
	})
}

func scrollbackPercent(row string) (int, bool) {
	m := scrollbackPercentSuffix.FindStringSubmatch(row)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return pct, true
}
