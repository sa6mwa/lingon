//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

var attachWheelUpSeq = []byte{0x1b, '[', '<', '6', '4', ';', '1', ';', '1', 'M'}
var attachWheelDownSeq = []byte{0x1b, '[', '<', '6', '5', ';', '1', ';', '1', 'M'}
var attachVisibleLinePattern = regexp.MustCompile(`LINE-(\d{2,3})`)

func TestAttachScrollbackDownMovesImmediately(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach_down"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	_ = host

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 80 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-80") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes([]byte{0x0c, '['})
	h.Advance(150 * time.Millisecond)

	foundTop := false
	for i := 0; i < 80; i++ {
		attach.SendBytes([]byte{0x1b, '[', 'A'})
		h.Advance(30 * time.Millisecond)
		if attach.Screen().Contains("LINE-01") {
			foundTop = true
			break
		}
	}
	if !foundTop {
		t.Fatalf("expected scrollback to reveal LINE-01; got:\n%s", attach.Screen().String())
	}

	for i := 0; i < 5; i++ {
		attach.SendBytes([]byte{0x1b, '[', 'A'})
		h.Advance(20 * time.Millisecond)
	}

	top := attach.Screen().String()
	attach.SendBytes([]byte{0x1b, '[', 'B'})
	h.Advance(120 * time.Millisecond)
	after := attach.Screen().String()
	if after == top {
		t.Fatalf("expected scroll down to move immediately from top; got unchanged screen:\n%s", after)
	}
}

func TestAttachScrollbackVimKeysControlViewport(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach_vim_keys"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	_ = host

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 80 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-80") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes([]byte{0x0c, '['})
	h.Advance(150 * time.Millisecond)

	attach.SendBytes([]byte("g"))
	waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 0
	})
	topLine := waitForAttachVisibleTopLine(t, h, attach, 2*time.Second, func(line int) bool {
		return line == 1
	})

	attach.SendBytes([]byte("J"))
	afterBigJ := waitForAttachVisibleTopLine(t, h, attach, 2*time.Second, func(line int) bool {
		return line == topLine+4
	})

	attach.SendBytes([]byte("j"))
	afterJ := waitForAttachVisibleTopLine(t, h, attach, 2*time.Second, func(line int) bool {
		return line == afterBigJ+1
	})

	attach.SendBytes([]byte("k"))
	afterK := waitForAttachVisibleTopLine(t, h, attach, 2*time.Second, func(line int) bool {
		return line == afterJ-1
	})
	if afterK != afterJ-1 {
		t.Fatalf("expected k to scroll up one line, got line %d after %d", afterK, afterJ)
	}

	attach.SendBytes([]byte("K"))
	waitForAttachVisibleTopLine(t, h, attach, 2*time.Second, func(line int) bool {
		return line == topLine
	})

	attach.SendBytes([]byte("G"))
	bottomPct := waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 100
	})
	attach.SendBytes([]byte("u"))
	afterU := waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct < bottomPct
	})

	attach.SendBytes([]byte("d"))
	afterD := waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct > afterU
	})
	if afterD <= afterU {
		t.Fatalf("expected d to page down, got percent %d after %d", afterD, afterU)
	}

	attach.SendBytes([]byte("g"))
	waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 0
	})
	attach.SendBytes([]byte{0x1b, '[', '5', '~'})
	waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 0
	})

	attach.SendBytes([]byte("G"))
	waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 100
	})
	attach.SendBytes([]byte{0x1b, '[', '6', '~'})
	waitForAttachScrollbackPercent(t, h, attach, 2*time.Second, func(pct int) bool {
		return pct == 100
	})
}

func TestAttachScrollbackEndDoesNotExit(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach_end"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	_ = host

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 80 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-80") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes([]byte{0x0c, '['})
	h.Advance(150 * time.Millisecond)

	attach.SendBytes([]byte{0x1b, '[', 'F'})
	h.Advance(80 * time.Millisecond)

	before := attach.Screen().String()
	attach.SendBytes([]byte{0x1b, '[', 'A'})
	h.Advance(120 * time.Millisecond)
	after := attach.Screen().String()
	if after == before {
		t.Fatalf("expected scrollback to remain active after End; got unchanged screen:\n%s", after)
	}
}

func TestAttachWheelRequiresCtrlLBracketToEnterAndWheelDownAutoExit(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	sessionID := "scroll_attach_wheel_auto"
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	_ = host

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	host.Send("i=1; while [ $i -le 80 ]; do printf 'LINE-%02d\\n' $i; i=$((i+1)); done\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LINE-80") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes(attachWheelUpSeq)
	h.Advance(120 * time.Millisecond)
	attach.Send("\nprintf 'LIVE_NO_AUTO_ENTER\\n'\n")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LIVE_NO_AUTO_ENTER") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	attach.SendBytes([]byte{0x0c, '['})
	h.Advance(120 * time.Millisecond)

	attach.Send("\nprintf 'BLOCKED_IN_SCROLLBACK\\n'\n")
	h.Advance(150 * time.Millisecond)
	if attach.Screen().Contains("BLOCKED_IN_SCROLLBACK") {
		t.Fatalf("expected input blocked while scrollback active")
	}

	attach.SendBytes([]byte{0x1b, '[', 'F'})
	h.Advance(100 * time.Millisecond)
	attach.Send("\nprintf 'STILL_BLOCKED_BY_END\\n'\n")
	h.Advance(150 * time.Millisecond)
	if attach.Screen().Contains("STILL_BLOCKED_BY_END") {
		t.Fatalf("expected End to keep scrollback active (no auto-exit)")
	}

	for i := 0; i < 120; i++ {
		attach.SendBytes(attachWheelDownSeq)
		h.Advance(20 * time.Millisecond)
	}

	attach.Send("\nprintf 'STILL_BLOCKED_AFTER_WHEEL_DOWN\\n'\n")
	h.Advance(150 * time.Millisecond)
	if attach.Screen().Contains("STILL_BLOCKED_AFTER_WHEEL_DOWN") {
		t.Fatalf("expected wheel down to stay in scrollback mode")
	}

	attach.SendBytes([]byte{'q'})
	h.Advance(80 * time.Millisecond)

	attach.Send("echo LIVE_AFTER_Q_EXIT\n")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LIVE_AFTER_Q_EXIT") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
}

var attachScrollbackPercentSuffix = regexp.MustCompile(`\[(\d{1,3})%\]\s*$`)

func waitForAttachScrollbackPercent(t *testing.T, h *ptytest.Harness, attach *ptytest.PTYSession, timeout time.Duration, want func(int) bool) int {
	t.Helper()
	deadline := ptytest.Now(h.Clock()).Add(timeout)
	lastRow := ""
	lastPct := 0
	for ptytest.Now(h.Clock()).Before(deadline) {
		row := attach.Screen().Row(0)
		lastRow = row
		pct, ok := attachScrollbackPercent(row)
		if ok {
			lastPct = pct
			if want(pct) {
				return pct
			}
		}
		h.Advance(20 * time.Millisecond)
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for attach scrollback percent; last row=%q pct=%d", lastRow, lastPct)
	return 0
}

func attachScrollbackPercent(row string) (int, bool) {
	m := attachScrollbackPercentSuffix.FindStringSubmatch(row)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return pct, true
}

func waitForAttachVisibleTopLine(t *testing.T, h *ptytest.Harness, attach *ptytest.PTYSession, timeout time.Duration, want func(int) bool) int {
	t.Helper()
	deadline := ptytest.Now(h.Clock()).Add(timeout)
	last := 0
	for ptytest.Now(h.Clock()).Before(deadline) {
		line, ok := attachVisibleTopLine(attach.Screen())
		if ok {
			last = line
			if want(line) {
				return line
			}
		}
		h.Advance(20 * time.Millisecond)
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for attach visible top line; last line=%d screen:\n%s", last, attach.Screen().String())
	return 0
}

func attachVisibleTopLine(screen ptytest.Screen) (int, bool) {
	for row := 1; row < screen.Rows; row++ {
		m := attachVisibleLinePattern.FindStringSubmatch(screen.Row(row))
		if len(m) != 2 {
			continue
		}
		line, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false
		}
		return line, true
	}
	return 0, false
}
