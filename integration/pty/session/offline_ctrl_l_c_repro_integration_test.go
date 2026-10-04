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

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestOfflineHostRenameUpdatesVisibleTabWithoutRelay(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("PS1", "PROMPT> ")
	clk := clock.NewMock()
	master, slave := ptytest.OpenPTY(t, 100, 24)
	host := ptytest.NewPTYSessionWithClock(t, master, slave, 100, 24, clk)
	t.Cleanup(host.Close)
	runner := session.New(session.Options{
		SessionID: "offline-original", SessionName: "offline-original", Shell: ctrlLShell(t),
		Term: "xterm-256color", Cols: 100, Rows: 24, Offline: true, Publish: false,
		Stdin: slave, Stdout: slave, Clock: clk,
	})
	go func() { host.SetRunErr(runner.Run(host.Context())) }()
	host.Send("echo READY\r")
	host.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("READY") || !strings.Contains(screen.Row(0), "offline-original") {
			return fmt.Errorf("initial tab/prompt not ready: %s", screen.String())
		}
		return nil
	})
	host.Send("\x0ceOffline renamed\r")
	host.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "Offline renamed") || strings.Contains(row, "offline-original") {
			return fmt.Errorf("rename did not update tab: %q", row)
		}
		return nil
	})
	host.Send("echo SAME_SHELL_OK\r")
	host.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SAME_SHELL_OK") {
			return fmt.Errorf("renamed terminal lost input")
		}
		return nil
	})
	host.Cancel()
	if exited, err := host.WaitErr(2 * time.Second); !exited || (err != nil && !errors.Is(err, context.Canceled)) {
		t.Fatalf("host shutdown: exited=%v err=%v", exited, err)
	}
}

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
