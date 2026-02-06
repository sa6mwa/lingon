package session_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestTabBarHidesWhenCursorOnTopRow(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-top-row",
		SessionName: "host-top-row",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.SendCtrlL()
	host.SendCtrlL()

	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after ctrl+l clear; got row %d col %d", cur.Row, cur.Col)
		}
		return nil
	})

	advanceTestClock(host.Clock(), 500*time.Millisecond)
	screen := host.Screen()
	if err := func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "host-top-row") {
			return fmt.Errorf("expected tab bar hidden when cursor is on top row; got %q", row)
		}
		return nil
	}(screen); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestTabBarHidesWhenCursorOnTopRowAfterWrapSwitch(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-top-row-wrap",
		SessionName: "host-top-row-wrap",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	host.SendCtrlL()
	host.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, host.Clock(), 4*time.Second, 100*time.Millisecond, func() error {
		host.SendCtrlL()
		host.SendCtrlL()
		advanceTestClock(h.Clock(), 100*time.Millisecond)
		row := host.Screen().Row(0)
		if strings.Contains(row, "host-top-row-wrap") {
			return fmt.Errorf("expected tab bar hidden on top row after wrap switch; got %q", row)
		}
		return nil
	})
}
