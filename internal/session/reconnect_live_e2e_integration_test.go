package session_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestReconnectCountdownTypingCursorAndPromptRemainStableE2E(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	// Real-clock harness to exercise production-like reconnect countdown updates.
	h := ptytest.New(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "reconnect-live-e2e",
		SessionName: "reconnect-live-e2e",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	host.Send("ls -lq")
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	h.StopServer()

	countdownRe := regexp.MustCompile(`reconnecting in (\d+)s`)
	seenCountdowns := map[string]bool{}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		screen := host.Screen()
		row0 := screen.Row(0)
		if !strings.Contains(row0, "connection lost") && !strings.Contains(row0, "reconnecting") {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if strings.Count(row0, "connection lost") > 1 || strings.Count(row0, "reconnecting") > 1 {
			t.Fatalf("reconnect banner text bleeds/duplicates on row 1: %q", row0)
		}
		if match := countdownRe.FindStringSubmatch(row0); len(match) == 2 {
			seenCountdowns[match[1]] = true
		}

		promptRows := make([]int, 0, 4)
		for i := 0; i < 10; i++ {
			if strings.Contains(screen.Row(i), "PROMPT>") {
				promptRows = append(promptRows, i+1)
			}
		}
		if len(promptRows) == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if len(promptRows) > 1 {
			t.Fatalf("prompt duplicated/drifted across rows during reconnect: rows=%v\nscreen:\n%s", promptRows, screen.String())
		}
		cur := host.Cursor()
		if cur.Row != promptRows[0] {
			t.Fatalf("cursor row does not match active prompt row during reconnect: cursor=%d prompt=%d\nscreen:\n%s", cur.Row, promptRows[0], screen.String())
		}
		time.Sleep(120 * time.Millisecond)
	}

	if len(seenCountdowns) < 2 {
		t.Fatalf("expected reconnect countdown to tick at least once, saw values=%v", seenCountdowns)
	}
}
