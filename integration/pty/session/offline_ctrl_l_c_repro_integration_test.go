//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestOfflineCtrlLCreateSwitchesAndShowsTabBarImmediately(t *testing.T) {
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("PS1", "PROMPT> ")

	master, slave := ptytest.OpenPTY(t, 140, 30)
	host := ptytest.NewPTYSession(t, master, slave, 140, 30)

	runner := session.New(session.Options{
		SessionID:   "offline-tmux-newpty",
		SessionName: "offline-tmux-newpty",
		Shell:       ctrlLShell(t),
		Term:        "tmux-256color",
		Cols:        140,
		Rows:        30,
		Publish:     false,
		Offline:     true,
		Stdin:       slave,
		Stdout:      slave,
	})
	go func() {
		host.SetRunErr(runner.Run(host.Context()))
	}()

	host.Send("echo READY\n")
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("READY") {
			return fmt.Errorf("expected READY output before ctrl+l c")
		}
		return nil
	})

	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "offline-tmux-newpty") {
			return fmt.Errorf("expected initial tab visible before ctrl+l c, got row %q", row)
		}
		return nil
	})

	host.SendCtrlL()
	host.Send("c")

	host.Eventually(500*time.Millisecond, 25*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "offline-tmux-newpty") || strings.Contains(row, "offline-tmux-newpty-2") {
			return fmt.Errorf("expected tab bar hidden when prompt owns row 1, got row %q", row)
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt visible on row 1 after ctrl+l c, got row %q", row)
		}
		return nil
	})

	host.Cancel()
	if exited, err := host.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}
