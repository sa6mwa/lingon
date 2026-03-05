package attach_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachScrollbackWheelUpReachesOlderRows(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach_wheel_regression"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 120 ]; do printf 'LINE-%03d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-120") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes([]byte{0x0c, '['})
	h.Advance(120 * time.Millisecond)

	foundOldest := false
	for i := 0; i < 80; i++ {
		attach.SendBytes([]byte{0x1b, '[', '<', '6', '4', ';', '1', ';', '1', 'M'})
		h.Advance(20 * time.Millisecond)
		if attach.Screen().Contains("LINE-001") {
			foundOldest = true
			break
		}
	}
	if !foundOldest {
		t.Fatalf("expected wheel-up scrollback to reach oldest rows; got:\n%s", attach.Screen().String())
	}
}
