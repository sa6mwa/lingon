package attach_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachScrollbackRestore(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	_ = host

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 60 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-60") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("connected to ") {
			return fmt.Errorf("waiting for connection banner to clear")
		}
		return nil
	})

	before := attach.Screen().String()

	attach.SendBytes([]byte{0x0c, '['})
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("[100%]") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		if row := screen.Row(0); row[len(row)-len("[100%]"):] != "[100%]" {
			return fmt.Errorf("expected scrollback indicator right-aligned, got %q", row)
		}
		return nil
	})

	found := false
	for i := 0; i < 6; i++ {
		attach.SendBytes([]byte{0x1b, '[', '5', '~'})
		h.Advance(75 * time.Millisecond)
		if attach.Screen().Contains("LINE-01") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scrollback to reveal LINE-01; got:\n%s", attach.Screen().String())
	}

	attach.Send("q")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if diff, ok := screen.Diff(before); !ok {
			return fmt.Errorf("screen mismatch after exit:\n%s", diff)
		}
		return nil
	})
}
