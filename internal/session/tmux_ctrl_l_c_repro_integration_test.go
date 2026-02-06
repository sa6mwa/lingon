package session_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestTMUXCtrlLCreateSwitchesAndShowsTabBar(t *testing.T) {
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-tmux-newpty",
		SessionName: "host-tmux-newpty",
		Shell:       ctrlLShell(t),
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if strings.Contains(row, "host-tmux-newpty") || strings.Contains(row, "host-tmux-newpty-2") {
			return fmt.Errorf("expected tab bar hidden when prompt owns row 1, got row %q", row)
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt visible on row 1 after ctrl+l c, got row %q", row)
		}
		return nil
	})
}

func TestTMUXCtrlLCreateDoesNotLeavePreviousPromptLineState(t *testing.T) {
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-tmux-prompt",
		SessionName: "host-tmux-prompt",
		Shell:       ctrlLShell(t),
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	// Leave editable text on the current prompt line without executing it.
	host.Send("echo STALE_INPUT")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if strings.Contains(row, "STALE_INPUT") {
			return fmt.Errorf("expected previous prompt line input to be cleared after ctrl+l c, got row %q", row)
		}
		return nil
	})
}
