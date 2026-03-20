package session_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestOnlineReconnectClearKeepsRow1FreeOfTabBar(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect",
		SessionName: "row1-reconnect",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got row %q", row)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	_ = host.DrainRaw()
	host.SendCtrlL()
	host.SendCtrlL()
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after clear, got row=%d col=%d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if strings.Contains(row, "row1-reconnect") || strings.Contains(row, "row1-reconnect-2") {
			return fmt.Errorf("tab bar rendered on row 1 during reconnect clear, got row %q", row)
		}
		if !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner to remain visible, got row %q", row)
		}
		cur = host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor to stay on row 1 with reconnect banner, got row=%d col=%d", cur.Row, cur.Col)
		}
		return nil
	})
}

func TestOnlineReconnectBannerKeepsCursorOnRow1(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-cursor",
		SessionName: "row1-reconnect-cursor",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after clear, got row=%d col=%d", cur.Row, cur.Col)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got row %q", row)
		}
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor to stay on row 1 while reconnect banner is visible, got row=%d col=%d", cur.Row, cur.Col)
		}
		return nil
	})
}

func TestOnlineReconnectBannerCountdownNoBleed(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-bleed",
		SessionName: "row1-reconnect-bleed",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	h.StopServer()

	waitBannerClean := func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		if strings.Count(row, "connection lost") > 1 || strings.Count(row, "reconnecting") > 1 {
			return fmt.Errorf("expected single reconnect banner text without bleed, got %q", row)
		}
		if strings.Contains(row, "coconnec") || strings.Contains(row, "reconnectingreconnecting") {
			return fmt.Errorf("expected no duplicated/garbled reconnect text, got %q", row)
		}
		return nil
	}

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, waitBannerClean)

	for i := 0; i < 5; i++ {
		advanceTestClock(h.Clock(), 1*time.Second)
		eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, waitBannerClean)
	}
}

func TestOnlineReconnectCountdownDoesNotDuplicatePromptRows(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-prompt-dup",
		SessionName: "row1-reconnect-prompt-dup",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt on row 1 before reconnect, got %q", row)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		return nil
	})

	countPromptRows := func() int {
		n := 0
		for i := 0; i < 10; i++ {
			if strings.Contains(host.Screen().Row(i), "PROMPT>") {
				n++
			}
		}
		return n
	}

	// DO NOT REMOVE: hard requirement.
	// Reconnect countdown ticks must not duplicate/drift the prompt vertically.
	// Ask the developer repeatedly before changing this assertion.
	for i := 0; i < 6; i++ {
		advanceTestClock(h.Clock(), 1*time.Second)
		eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
			n := countPromptRows()
			if n > 1 {
				return fmt.Errorf("expected at most one prompt row during reconnect countdown, got %d", n)
			}
			return nil
		})
	}
}

func TestOnlineReconnectTypingDuringCountdownDoesNotDuplicatePromptRows(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-prompt-typing-dup",
		SessionName: "row1-reconnect-prompt-typing-dup",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt on row 1 before reconnect, got %q", row)
		}
		return nil
	})

	h.StopServer()
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		return nil
	})

	host.Send("ls -lq")

	countPromptRows := func() int {
		n := 0
		for i := 0; i < 12; i++ {
			if strings.Contains(host.Screen().Row(i), "PROMPT>") {
				n++
			}
		}
		return n
	}

	for i := 0; i < 6; i++ {
		advanceTestClock(h.Clock(), 1*time.Second)
		eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
			n := countPromptRows()
			if n > 1 {
				return fmt.Errorf("expected at most one prompt row while typing during reconnect countdown, got %d", n)
			}
			return nil
		})
	}
}

func TestOnlineReconnectBannerPreservesPromptLeftOfBadgeOnRowOne(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-prompt-preserve",
		SessionName: "row1-reconnect-prompt-preserve",
		Shell:       reconnectShell(t),
		Cols:        220,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt on row 1 before reconnect, got %q", row)
		}
		return nil
	})

	h.StopServer()
	host.SendCtrlL()
	host.SendCtrlL()

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		reconnectIdx := strings.Index(row, "connection lost")
		if reconnectIdx < 0 {
			reconnectIdx = strings.Index(row, "reconnecting")
		}
		if reconnectIdx < 0 {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		promptIdx := strings.Index(row, "PROMPT>")
		if promptIdx < 0 {
			return fmt.Errorf("expected prompt text preserved on row 1 while reconnect banner visible, got %q", row)
		}
		if reconnectIdx <= promptIdx {
			return fmt.Errorf("expected reconnect banner to remain right-aligned of prompt, got %q", row)
		}
		return nil
	})
}

func TestOnlineReconnectBannerKeepsTabBarVisibleWhenCursorNotTopRow(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-reconnect-tabs",
		SessionName: "row1-reconnect-tabs",
		Shell:       reconnectShell(t),
		Cols:        220,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	host.Send("echo cursor-below-top-row\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if cur := host.Cursor(); cur.Row <= 1 {
			return fmt.Errorf("expected cursor below row 1 before reconnect, got row=%d col=%d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if !strings.Contains(row, "row1-reconnect-tabs") {
			return fmt.Errorf("expected tab bar visible before reconnect, got row=%q", row)
		}
		return nil
	})

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		reconnectIdx := strings.Index(row, "connection lost")
		if reconnectIdx < 0 {
			reconnectIdx = strings.Index(row, "reconnecting")
		}
		if reconnectIdx < 0 {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		tabIdx := strings.Index(row, "row1-reconnect-tabs")
		if tabIdx < 0 {
			return fmt.Errorf("expected tab bar text to remain visible with reconnect banner, got %q", row)
		}
		if tabIdx >= reconnectIdx {
			return fmt.Errorf("expected tab label to remain left of reconnect banner badge, got %q", row)
		}
		return nil
	})
}
