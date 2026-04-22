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

func TestOfflineCloseSessionRemovesClosedTabLabel(t *testing.T) {
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("PS1", "PROMPT> ")

	master, slave := ptytest.OpenPTY(t, 140, 30)
	host := ptytest.NewPTYSession(t, master, slave, 140, 30)

	runner := session.New(session.Options{
		SessionID:   "close-local",
		SessionName: "close-local",
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
			return fmt.Errorf("expected READY output before close test")
		}
		return nil
	})

	host.SendCtrlL()
	host.Send("c")
	host.Send("echo SECOND\n")
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("SECOND") {
			return fmt.Errorf("expected SECOND output on created session before close")
		}
		return nil
	})

	host.SendCtrlL()
	host.Send("Q")
	host.Send("echo AFTER_CLOSE\n")

	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "close-local-2") {
			return fmt.Errorf("expected closed tab label removed after ctrl+l Q, got row %q", row)
		}
		return nil
	})

	host.Cancel()
	if exited, err := host.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}
