//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

var styledLineClearAtCol1Re = regexp.MustCompile(`\x1b\[(\d+);1H(?:\x1b\[[0-9;]*m)*\x1b\[(?:[0-2])?K`)
var cursorAtCol1Re = regexp.MustCompile(`\x1b\[(\d+);1H`)

const baseTopRowResetSeq = "\x1b[1;1H\x1b[0;39;49m"

func TestHostNoFullRedrawWhileOffline(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	clk := clock.New()
	authPath := filepath.Join(t.TempDir(), "auth.json")
	state := authstore.State{
		Endpoint:         "https://127.0.0.1:1/v1",
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatalf("save auth: %v", err)
	}

	master, slave := ptytest.OpenPTY(t, 80, 24)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, 80, 24, clk)
	runner := session.New(session.Options{
		Endpoint:  state.Endpoint,
		Token:     "",
		AuthFile:  authPath,
		SessionID: "offline-host",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
		Publish:   true,
		Stdin:     slave,
		Stdout:    slave,
		Clock:     clk,
	})
	go func() {
		sess.SetRunErr(runner.Run(sess.Context()))
	}()

	advanceTestClock(sess.Clock(), 200*time.Millisecond)
	sess.Send("echo READY\n")
	waitForRawContains(t, sess, "READY", 2*time.Second, 50*time.Millisecond, "expected READY output while relay is down")

	assertSingleTopBanner(t, sess)
	assertNoFullRedraw(t, sess, 24, 2*time.Second)
	sess.Cancel()
	if exited, err := sess.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}

func TestHostNoFullRedrawAfterRelayDrop(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-drop",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo READY\n")
	waitForRawContains(t, host, "READY", 2*time.Second, 50*time.Millisecond, "expected READY output before drop")

	h.StopServer()
	assertSingleTopBanner(t, host)
	assertNoFullRedraw(t, host, 24, 2*time.Second)
}

func TestHostReconnectDoesNotRepaintBaseTopRowBeforeOverlay(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-reconnect-tab-flicker",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.Send("echo RECONNECT_TABBAR_BASELINE\n")
	waitForRawContains(t, host, "RECONNECT_TABBAR_BASELINE", 2*time.Second, 50*time.Millisecond, "expected baseline output before reconnect")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if cur := host.Cursor(); cur.Row <= 1 {
			return fmt.Errorf("expected cursor below row 1 before reconnect, got row=%d col=%d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if !strings.Contains(row, "host-reconnect-tab-flicker") {
			return fmt.Errorf("expected tab bar visible before reconnect, got row=%q", row)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	h.StopServer()
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if row := host.Screen().Row(0); !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner, got row=%q", row)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)
	_ = host.DrainRaw()
	advanceTestClock(h.Clock(), 1200*time.Millisecond)
	raw := host.DrainRaw()
	if strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("reconnect tick repainted full tab row; expected badge-only update, raw=%q", truncateRaw(raw))
	}
}

func TestHostResizeDoesNotClearScreen(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-resize-no-clear",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo RESIZE_BASELINE\n")
	waitForRawContains(t, host, "RESIZE_BASELINE", 2*time.Second, 50*time.Millisecond, "expected baseline output before resize")
	waitForRawIdle(t, host, 120*time.Millisecond, 2*time.Second)

	assertNoClearScreenAfterAction(t, host, 1200*time.Millisecond, func() {
		host.Resize(100, 30)
	})
}

func TestHostLocalOnlyNoFullRedrawOnWrapAroundTabSwitch(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	master, slave := ptytest.OpenPTY(t, 80, 24)
	sess := ptytest.NewPTYSession(t, master, slave, 80, 24)
	runner := session.New(session.Options{
		SessionID: "local-wrap-only",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
		Publish:   false,
		Stdin:     slave,
		Stdout:    slave,
	})
	go func() {
		sess.SetRunErr(runner.Run(sess.Context()))
	}()

	advanceTestClock(sess.Clock(), 300*time.Millisecond)
	sess.Send("echo LOCAL_READY\n")
	waitForRawContains(t, sess, "LOCAL_READY", 2*time.Second, 50*time.Millisecond, "expected LOCAL_READY output")

	sess.SendCtrlL()
	sess.Send("c")
	advanceTestClock(sess.Clock(), 250*time.Millisecond)
	sess.Send("echo SECOND_READY\n")
	waitForRawContains(t, sess, "SECOND_READY", 2*time.Second, 50*time.Millisecond, "expected SECOND_READY output on second tab")
	waitForRawIdle(t, sess, 150*time.Millisecond, 2*time.Second)

	assertNoTabSwitchFlickerAfterAction(t, sess, 24, 800*time.Millisecond, func() {
		sess.SendCtrlL()
		sess.Send("n")
	})

	sess.Cancel()
	if exited, err := sess.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}

func TestHostNoFullRedrawOnHelpToggle(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-help",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	advanceTestClock(host.Clock(), 200*time.Millisecond)
	ready := false
	for i := 0; i < 3; i++ {
		host.Send("echo READY\n")
		if waitForRawContainsOptional(host, "READY", 1500*time.Millisecond, 50*time.Millisecond) {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("expected READY output before help toggle")
	}

	waitForRawIdle(t, host, 100*time.Millisecond, 2*time.Second)
	_ = host.DrainRaw()
	host.SendCtrlL()
	host.Send("h")
	assertNoFullRedraw(t, host, 24, 400*time.Millisecond)

	waitForRawIdle(t, host, 100*time.Millisecond, 2*time.Second)
	_ = host.DrainRaw()
	host.Send("q")
	assertNoFullRedraw(t, host, 24, 400*time.Millisecond)
}

func TestHostNoFullRedrawOnOfflineWrapAroundTabSwitch(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-offline-wrap",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.SendCtrlL()
	host.Send("o")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.SendCtrlL()
	host.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.SendCtrlL()
	host.Send("o")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.SendCtrlL()
	host.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	assertNoTabSwitchFlickerAfterAction(t, host, 24, 800*time.Millisecond, func() {
		host.SendCtrlL()
		host.Send("n")
	})
}

func TestHostNoFullRedrawOnOnlineWrapAroundTabSwitch(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-online-wrap",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	assertNoTabSwitchFlickerAfterAction(t, host, 24, 800*time.Millisecond, func() {
		host.SendCtrlL()
		host.Send("n")
	})
}

func TestHostNoFullRedrawOnScrollbackPaging(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	master, slave := ptytest.OpenPTY(t, 80, 24)
	sess := ptytest.NewPTYSession(t, master, slave, 80, 24)
	runner := session.New(session.Options{
		SessionID: "local-scrollback-no-flicker",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
		Publish:   false,
		Stdin:     slave,
		Stdout:    slave,
	})
	go func() {
		sess.SetRunErr(runner.Run(sess.Context()))
	}()

	advanceTestClock(sess.Clock(), 300*time.Millisecond)
	sess.Send("i=1; while [ $i -le 120 ]; do printf 'SCROLL-%03d\\n' $i; i=$((i+1)); done\n")
	sess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SCROLL-120") {
			return errors.New("expected SCROLL-120 output")
		}
		return nil
	})
	waitForRawIdle(t, sess, 150*time.Millisecond, 2*time.Second)

	sess.SendBytes([]byte{0x0c, '['})
	advanceTestClock(sess.Clock(), 120*time.Millisecond)
	waitForRawIdle(t, sess, 100*time.Millisecond, 2*time.Second)

	assertNoScrollbackFlickerAfterAction(t, sess, 24, 600*time.Millisecond, func() {
		sess.SendBytes([]byte{0x1b, '[', 'A'})
	})

	sess.Cancel()
	if exited, err := sess.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}

func assertNoFullRedraw(t *testing.T, sess *ptytest.PTYSession, rows int, d time.Duration) {
	t.Helper()
	_ = sess.DrainRaw()
	deadline := sess.Clock().Now().Add(d)
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		if ptytest.HasFullRedrawANSI(raw, rows) {
			t.Fatalf("unexpected full-screen redraw while overlay active: %q", truncateRaw(raw))
		}
		advanceTestClock(sess.Clock(), 200*time.Millisecond)
	}
}

func assertNoFullRedrawAfterAction(t *testing.T, sess *ptytest.PTYSession, rows int, d time.Duration, action func()) {
	t.Helper()
	_ = sess.DrainRaw()
	action()
	deadline := sess.Clock().Now().Add(d)
	window := ""
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if strings.Contains(window, "\x1b[2J") || ptytest.HasFullRedrawANSI(window, rows) {
			t.Fatalf("unexpected full-screen redraw during action: %q", truncateRaw(window))
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
}

// NON-NEGOTIABLE INVARIANT FOR TAB SWITCHING:
// DO NOT REMOVE THIS ASSERTION OR WATER IT DOWN.
// Tab switch must not visibly repaint the top row twice (base row then tab row).
// ASK THE DEVELOPER THREE TIMES BEFORE TOUCHING THIS.
func assertNoTabSwitchFlickerAfterAction(t *testing.T, sess *ptytest.PTYSession, rows int, d time.Duration, action func()) {
	t.Helper()
	_ = sess.DrainRaw()
	action()
	deadline := sess.Clock().Now().Add(d)
	window := ""
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if strings.Contains(window, "\x1b[2J") || ptytest.HasFullRedrawANSI(window, rows) || hasStyledFullRedraw(window, rows) || hasFullViewportRepaint(window, rows) {
			t.Fatalf("unexpected full-screen redraw during action: %q", truncateRaw(window))
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
	if hasBaseTopRowBeforeTabOverlay(window) {
		t.Fatalf("tab switch repainted base top row before overlay; expected overlay-composed output: %q", truncateRaw(window))
	}
}

// NON-NEGOTIABLE INVARIANT FOR SCROLLBACK NAVIGATION:
// DO NOT REMOVE THIS ASSERTION OR WATER IT DOWN.
// Scrolling inside ctrl+l [ mode must not visibly repaint base row 1 before overlay.
// ASK THE DEVELOPER THREE TIMES BEFORE TOUCHING THIS.
func assertNoScrollbackFlickerAfterAction(t *testing.T, sess *ptytest.PTYSession, rows int, d time.Duration, action func()) {
	t.Helper()
	_ = sess.DrainRaw()
	action()
	deadline := sess.Clock().Now().Add(d)
	window := ""
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if strings.Contains(window, "\x1b[2J") || ptytest.HasFullRedrawANSI(window, rows) {
			t.Fatalf("unexpected full-screen redraw during scrollback action: %q", truncateRaw(window))
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
	if strings.Contains(window, baseTopRowResetSeq) {
		t.Fatalf("scrollback repainted base top row before overlay; expected overlay-composed output: %q", truncateRaw(window))
	}
}

func assertNoClearScreenAfterAction(t *testing.T, sess *ptytest.PTYSession, d time.Duration, action func()) {
	t.Helper()
	_ = sess.DrainRaw()
	action()
	deadline := sess.Clock().Now().Add(d)
	window := ""
	for sess.Clock().Now().Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if strings.Contains(window, "\x1b[2J") {
			t.Fatalf("unexpected clear-screen during resize action: %q", truncateRaw(window))
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
}

func hasBaseTopRowBeforeTabOverlay(window string) bool {
	tabOverlay := "\x1b[1;1H\x1b[0m\x1b[48;"
	baseIdx := strings.Index(window, baseTopRowResetSeq)
	if baseIdx < 0 {
		return false
	}
	tabIdx := strings.Index(window, tabOverlay)
	if tabIdx < 0 {
		return true
	}
	return baseIdx < tabIdx
}

func hasStyledFullRedraw(data string, rows int) bool {
	if rows <= 0 {
		return false
	}
	rowsSeen := make(map[int]struct{}, rows)
	for _, match := range styledLineClearAtCol1Re.FindAllStringSubmatch(data, -1) {
		if len(match) < 2 {
			continue
		}
		row, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		rowsSeen[row] = struct{}{}
	}
	return len(rowsSeen) >= rows-1
}

func hasFullViewportRepaint(data string, rows int) bool {
	if rows <= 0 {
		return false
	}
	rowsSeen := make(map[int]struct{}, rows)
	for _, match := range cursorAtCol1Re.FindAllStringSubmatch(data, -1) {
		if len(match) < 2 {
			continue
		}
		row, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		rowsSeen[row] = struct{}{}
	}
	return len(rowsSeen) >= rows-1
}

func waitForRawContains(t *testing.T, sess *ptytest.PTYSession, substr string, timeout, step time.Duration, msg string) {
	t.Helper()
	deadline := sess.Clock().Now().Add(timeout)
	var seen strings.Builder
	for sess.Clock().Now().Before(deadline) {
		seen.WriteString(sess.DrainRaw())
		if strings.Contains(seen.String(), substr) {
			return
		}
		advanceTestClock(sess.Clock(), step)
	}
	t.Fatalf("%s", msg)
}

func waitForRawContainsOptional(sess *ptytest.PTYSession, substr string, timeout, step time.Duration) bool {
	deadline := sess.Clock().Now().Add(timeout)
	var seen strings.Builder
	for sess.Clock().Now().Before(deadline) {
		seen.WriteString(sess.DrainRaw())
		if strings.Contains(seen.String(), substr) {
			return true
		}
		advanceTestClock(sess.Clock(), step)
	}
	seen.WriteString(sess.DrainRaw())
	return strings.Contains(seen.String(), substr)
}

func waitForRawIdle(t *testing.T, sess *ptytest.PTYSession, idle, timeout time.Duration) {
	t.Helper()
	if idle <= 0 {
		return
	}
	start := time.Now()
	idleStart := time.Now()
	for time.Since(start) < timeout {
		if raw := sess.DrainRaw(); raw != "" {
			idleStart = time.Now()
		}
		if time.Since(idleStart) >= idle {
			return
		}
		advanceTestClock(sess.Clock(), 100*time.Millisecond)
	}
	t.Fatalf("timed out waiting for raw output to idle")
}

func assertSingleTopBanner(t *testing.T, sess *ptytest.PTYSession) {
	t.Helper()
	screen := sess.Screen()
	row1 := screen.Row(0)
	row2 := screen.Row(1)
	count := 0
	if strings.Contains(row1, "connection lost") || strings.Contains(row1, "reconnecting") {
		count++
	}
	if strings.Contains(row2, "connection lost") || strings.Contains(row2, "reconnecting") {
		count++
	}
	if count > 1 {
		t.Fatalf("expected a single top-row disconnect banner; row1=%q row2=%q", row1, row2)
	}
}

func truncateRaw(raw string) string {
	if len(raw) <= 200 {
		return raw
	}
	return raw[:200]
}
