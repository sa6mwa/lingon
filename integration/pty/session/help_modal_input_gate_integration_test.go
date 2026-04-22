//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHelpModalBlocksInputUntilDismissed(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-help-input-gate",
		SessionName: "host-help-input-gate",
		Shell:       ctrlLShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo READY\n")
	if !screenContainsWithin(host, "READY", 2*time.Second) {
		t.Fatalf("expected READY before help modal")
	}

	host.SendCtrlL()
	host.Send("h")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains(mvu.HelpTitle()) {
			return fmt.Errorf("expected help modal visible")
		}
		return nil
	})

	const blockedToken = "BLOCKED_BY_HELP_MODAL"
	host.Send("echo " + blockedToken + "\n")
	host.ExpectAfter(700*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains(blockedToken) {
			return fmt.Errorf("expected input to be blocked while help is visible")
		}
		return nil
	})

	host.Send("q")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if host.Screen().Contains(mvu.HelpTitle()) {
			return fmt.Errorf("expected help modal dismissed")
		}
		return nil
	})

	host.ExpectAfter(700*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains(blockedToken) {
			return fmt.Errorf("expected blocked input to be discarded after help dismiss")
		}
		return nil
	})

	host.Send("echo AFTER_HELP\n")
	if !screenContainsWithin(host, "AFTER_HELP", 2*time.Second) {
		t.Fatalf("expected input accepted after help dismiss")
	}
}

func TestHelpModalDismissKeysAndRejectedEscEnter(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-help-dismiss-keys",
		SessionName: "host-help-dismiss-keys",
		Shell:       ctrlLShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	showHelp := func() {
		host.SendCtrlL()
		host.Send("h")
		eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
			if !host.Screen().Contains(mvu.HelpTitle()) {
				return fmt.Errorf("expected help modal visible")
			}
			return nil
		})
	}
	helpHidden := func() error {
		if host.Screen().Contains(mvu.HelpTitle()) {
			return fmt.Errorf("expected help modal hidden")
		}
		return nil
	}

	showHelp()
	host.Send("Q")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, helpHidden)

	showHelp()
	host.SendBytes([]byte{0x1b})
	host.ExpectAfter(700*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(mvu.HelpTitle()) {
			return fmt.Errorf("expected help modal to remain visible after ESC")
		}
		return nil
	})

	host.Send("\n")
	host.ExpectAfter(700*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(mvu.HelpTitle()) {
			return fmt.Errorf("expected help modal to remain visible after Enter")
		}
		return nil
	})
	host.Send("q")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, helpHidden)
}
