package attach_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachCountdownOnServerDisconnect(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_disconnect_countdown",
		SessionName: "session_disconnect_countdown",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_disconnect_countdown"})
	waitForHost(t, h, "session_disconnect_countdown", 10*time.Second)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "session_disconnect_countdown",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attach.Cancel)
	if exited, err := attach.WaitErr(200 * time.Millisecond); exited {
		t.Fatalf("attach exited early: %v", err)
	}
	waitForClientCount(t, h, "session_disconnect_countdown", 1, 3*time.Second)
	h.Advance(200 * time.Millisecond)
	host.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.TrimSpace(screen.String()) == "" {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})
	host.Send("printf 'ready-countdown\\n'\n")
	host.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("ready-countdown") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("ready-countdown") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	_ = attach.DrainRaw()
	h.StopServer()

	initialCountdown := regexp.MustCompile(`reconnecting in (\d+)s`)
	var initialVal int
	found := false
	for i := 0; i < 6; i++ {
		h.Advance(200 * time.Millisecond)
		screen := attach.Screen()
		if !screen.Contains("Not connected") {
			continue
		}
		matches := initialCountdown.FindStringSubmatch(screen.String())
		if len(matches) < 2 {
			continue
		}
		val, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		initialVal = val
		found = true
		break
	}
	if !found {
		t.Fatalf("expected disconnect overlay after disconnect, got:\n%s", attach.Screen().String())
	}

	_ = attach.DrainRaw()

	countdownRe := regexp.MustCompile(`reconnecting in (\d+)s`)
	h.Advance(1100 * time.Millisecond)
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		matches := countdownRe.FindStringSubmatch(screen.String())
		if len(matches) < 2 {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		val, err := strconv.Atoi(matches[1])
		if err != nil {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		if val < 0 || val > initialVal {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
}
