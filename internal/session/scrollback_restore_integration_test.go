package session_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostScrollbackRestore(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host",
		SessionName: "scroll_host",
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})

	waitForHost(t, h, "scroll_host", 3*time.Second)

	host.Send("i=1; while [ $i -le 60 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("LINE-60") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})

	before := host.Screen().String()

	host.SendBytes([]byte{0x0c, '['})
	waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
		if !strings.Contains(row, "[100%]") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		if row[len(row)-len("[100%]"):] != "[100%]" {
			return fmt.Errorf("expected scrollback indicator right-aligned, got %q", row)
		}
		return nil
	})

	found := false
	for i := 0; i < 6; i++ {
		host.SendBytes([]byte{0x1b, '[', '5', '~'})
		advanceTestClock(h.Clock(), 75*time.Millisecond)
		if host.Screen().Contains("LINE-01") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scrollback to reveal LINE-01; got:\n%s", host.Screen().String())
	}

	host.Send("q")
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if diff, ok := screen.Diff(before); !ok {
			return fmt.Errorf("screen mismatch after exit:\n%s", diff)
		}
		return nil
	})
}

func waitForHost(t *testing.T, h *ptytest.Harness, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := h.Clock().Now().Add(timeout)
	for h.Clock().Now().Before(deadline) {
		if h.HasHost(sessionID) {
			return
		}
		advanceTestClock(h.Clock(), 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for host %q", sessionID)
}
