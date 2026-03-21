package session_test

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

var hostWheelUpSeq = []byte{0x1b, '[', '<', '6', '4', ';', '1', ';', '1', 'M'}
var hostWheelDownSeq = []byte{0x1b, '[', '<', '6', '5', ';', '1', ';', '1', 'M'}

func TestHostScrollbackDownMovesImmediately(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host_down",
		SessionName: "scroll_host_down",
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})

	waitForHost(t, h, "scroll_host_down", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostCommandReady(t, host, "__SCROLL_DOWN_READY__", 3*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("emit-lines LINE 2 80\n")
	waitForStableSeededHostOutput(t, host, "LINE-80", 3*time.Second)

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	foundTop := false
	for i := 0; i < 80; i++ {
		host.SendBytes([]byte{0x1b, '[', 'A'})
		advanceTestClock(h.Clock(), 30*time.Millisecond)
		if host.Screen().Contains("LINE-01") {
			foundTop = true
			break
		}
	}
	if !foundTop {
		t.Fatalf("expected scrollback to reveal LINE-01; got:\n%s", host.Screen().String())
	}

	for i := 0; i < 5; i++ {
		host.SendBytes([]byte{0x1b, '[', 'A'})
		advanceTestClock(h.Clock(), 20*time.Millisecond)
	}

	top := host.Screen().String()
	host.SendBytes([]byte{0x1b, '[', 'B'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	after := host.Screen().String()
	if after == top {
		t.Fatalf("expected scroll down to move immediately from top; got unchanged screen:\n%s", after)
	}
}

func TestHostScrollbackEndDoesNotExit(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host_end",
		SessionName: "scroll_host_end",
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})

	waitForHost(t, h, "scroll_host_end", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostCommandReady(t, host, "__SCROLL_END_READY__", 3*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("emit-lines LINE 2 80\n")
	waitForStableSeededHostOutput(t, host, "LINE-80", 3*time.Second)

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	host.SendBytes([]byte{0x1b, '[', 'F'})
	advanceTestClock(h.Clock(), 80*time.Millisecond)

	before := host.Screen().String()
	host.SendBytes([]byte{0x1b, '[', 'A'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	after := host.Screen().String()
	if after == before {
		t.Fatalf("expected scrollback to remain active after End; got unchanged screen:\n%s", after)
	}
}

func TestHostWheelRequiresCtrlLBracketToEnterAndWheelDownAutoExit(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host_wheel_auto",
		SessionName: "scroll_host_wheel_auto",
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})

	waitForHost(t, h, "scroll_host_wheel_auto", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostCommandReady(t, host, "__SCROLL_WHEEL_READY__", 3*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("emit-lines LINE 2 80\n")
	waitForStableSeededHostOutput(t, host, "LINE-80", 3*time.Second)

	host.SendBytes(hostWheelUpSeq)
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.Send("emit LIVE_NO_AUTO_ENTER\n")
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("LIVE_NO_AUTO_ENTER") {
			return ptytest.FormatRowDiff("host", 0, host.Screen().Row(0))
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)

	host.Send("emit BLOCKED_IN_SCROLLBACK\n")
	advanceTestClock(h.Clock(), 150*time.Millisecond)
	if host.Screen().Contains("BLOCKED_IN_SCROLLBACK") {
		t.Fatalf("expected input blocked while scrollback active")
	}

	host.SendBytes([]byte{0x1b, '[', 'F'})
	advanceTestClock(h.Clock(), 100*time.Millisecond)
	host.Send("emit STILL_BLOCKED_BY_END\n")
	advanceTestClock(h.Clock(), 150*time.Millisecond)
	if host.Screen().Contains("STILL_BLOCKED_BY_END") {
		t.Fatalf("expected End to keep scrollback active (no auto-exit)")
	}

	for i := 0; i < 120; i++ {
		host.SendBytes(hostWheelDownSeq)
		advanceTestClock(h.Clock(), 20*time.Millisecond)
	}

	host.Send("emit STILL_BLOCKED_AFTER_WHEEL_DOWN\n")
	advanceTestClock(h.Clock(), 150*time.Millisecond)
	if host.Screen().Contains("STILL_BLOCKED_AFTER_WHEEL_DOWN") {
		t.Fatalf("expected wheel down to stay in scrollback mode")
	}

	host.SendBytes([]byte{'q'})
	advanceTestClock(h.Clock(), 80*time.Millisecond)

	host.Send("echo LIVE_AFTER_Q_EXIT\n")
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("LIVE_AFTER_Q_EXIT") {
			return ptytest.FormatRowDiff("host", 0, host.Screen().Row(0))
		}
		return nil
	})
}

func TestHostScrollbackIndicatorRightAlignedNoBleedFrom100To0(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	const cols = 80
	sessionID := "scroll_host_indicator"
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        cols,
		Rows:        20,
	})

	waitForHost(t, h, sessionID, 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostCommandReady(t, host, "__SCROLL_INDICATOR_READY__", 3*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)
	host.Send("emit-lines LINE 3 140\n")
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("LINE-140") {
			return ptytest.FormatRowDiff("host", 0, host.Screen().Row(0))
		}
		return nil
	})
	advanceTestClock(h.Clock(), 4*time.Second)
	eventuallyWithClock(t, host.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if strings.Contains(row, "connected to ") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)

	assertRightAligned := func(token string) {
		t.Helper()
		row := waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
			if !strings.Contains(row, token) {
				return ptytest.FormatRowDiff("host", 0, row)
			}
			return nil
		})
		if strings.Contains(row, sessionID) {
			t.Fatalf("expected tab bar hidden in scrollback, got row=%q", row)
		}
		start := cols - len(token)
		if start < 0 {
			start = 0
		}
		if row[start:] != token {
			t.Fatalf("expected %q right-aligned on row1, got row=%q", token, row)
		}
		if start > 0 {
			bg, ok := host.CellBG(1, start)
			if ok && bg != terminal.ColorDefault {
				t.Fatalf("expected no color bleed before indicator start, got bg=%#x row=%q", bg, row)
			}
		}
		bgToken, ok := host.CellBG(1, start+1)
		if ok && bgToken == terminal.ColorDefault {
			t.Fatalf("expected colored indicator token area, got default bg row=%q", row)
		}
	}

	assertRightAligned("[100%]")

	for i := 0; i < 180; i++ {
		host.SendBytes([]byte{0x1b, '[', 'A'})
		advanceTestClock(h.Clock(), 15*time.Millisecond)
	}

	row := waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
		if !strings.Contains(row, "[0%]") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		return nil
	})
	// These columns were occupied by "[100%]" but must be cleared when rendering "[0%]".
	if row[cols-6:cols-4] != "  " {
		t.Fatalf("expected cleared columns when moving from [100%%] to [0%%], got row=%q", row)
	}
	assertRightAligned("[0%]")
}

func waitForStableSeededHostOutput(t *testing.T, host *ptytest.PTYSession, token string, timeout time.Duration) {
	t.Helper()
	eventuallyWithClock(t, host.Clock(), timeout, 50*time.Millisecond, func() error {
		if !host.Screen().Contains(token) {
			return ptytest.FormatRowDiff("host", 0, host.Screen().Row(0))
		}
		return nil
	})
	waitForHostPromptIdle(t, host, timeout, 50*time.Millisecond, 3)
}

func waitForHostCommandReady(t *testing.T, host *ptytest.PTYSession, marker string, timeout time.Duration) {
	t.Helper()
	host.Send("emit " + marker + "\n")
	waitForRawContains(t, host, marker, timeout, 50*time.Millisecond, "expected host command-ready marker")
}

func waitForConnectedBannerClear(t *testing.T, host *ptytest.PTYSession, timeout time.Duration) {
	t.Helper()
	eventuallyWithClock(t, host.Clock(), timeout, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if screen.Contains("connected to ") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})
}

func waitForHostPromptIdle(t *testing.T, host *ptytest.PTYSession, timeout, step time.Duration, stableSteps int) {
	t.Helper()
	if stableSteps <= 0 {
		stableSteps = 1
	}
	deadline := host.Clock().Now().Add(timeout)
	quiet := 0
	for host.Clock().Now().Before(deadline) {
		raw := host.DrainRaw()
		if raw == "" && host.Screen().Contains("PROMPT> ") {
			quiet++
			if quiet >= stableSteps {
				return
			}
		} else {
			quiet = 0
		}
		advanceTestClock(host.Clock(), step)
	}
	t.Fatalf("timed out waiting for host prompt to become idle")
}

func TestHostScrollbackIndicatorSuppressesReconnectBanner(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	const cols = 120
	sessionID := "scroll_host_reconnect_suppressed"
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        cols,
		Rows:        24,
	})
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.Send("i=1; while [ $i -le 80 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	eventuallyWithClock(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("LINE-80") {
			return ptytest.FormatRowDiff("host", 0, host.Screen().Row(0))
		}
		return nil
	})

	h.StopServer()
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)

	waitForStableTopRow(t, host, 2*time.Second, 50*time.Millisecond, 3, func(row string) error {
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		if !strings.Contains(row, "[") || !strings.Contains(row, "%]") {
			return ptytest.FormatRowDiff("host", 0, row)
		}
		return nil
	})
}
