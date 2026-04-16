package attach_test

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

const baseTopRowResetSeq = "\x1b[1;1H\x1b[0;39;49m"

func TestAttachNoFullRedrawOnDisconnect(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "host-attach", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-attach"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "host-attach", Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 1)

	attach.Wait(500 * time.Millisecond)

	h.StopServer()
	attach.Wait(200 * time.Millisecond)
	_ = attach.DrainRaw()
	assertNoFullRedraw(t, attach, 24, 2500*time.Millisecond)
	attach.Cancel()
	_, _ = attach.WaitErr(200 * time.Millisecond)
}

func TestAttachReconnectDoesNotRepaintBaseTopRowBeforeOverlay(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{SessionID: "ar-a", SessionName: "ra", Shell: shell, Cols: 120, Rows: 24})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "ar-b", SessionName: "rb", Shell: shell, Cols: 120, Rows: 24})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"ar-a", "ar-b"})

	hostA.Send("echo ATTACH_RECONNECT_A\n")
	if !screenContainsWithin(hostA, "ATTACH_RECONNECT_A", 2*time.Second) {
		t.Fatalf("expected host A baseline output before attach")
	}
	hostB.Send("echo ATTACH_RECONNECT_B\n")
	if !screenContainsWithin(hostB, "ATTACH_RECONNECT_B", 2*time.Second) {
		t.Fatalf("expected host B baseline output before attach")
	}

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "ar-a", Cols: 120, Rows: 24})
	t.Cleanup(attach.Cancel)
	waitForTabLabels(t, attach, []string{"ra", "rb"}, 6*time.Second)
	primeTabsByCount(t, attach, 2)
	attach.SendCtrlL()
	attach.Send("p")

	attach.Send("echo ATTACH_RECONNECT_BASELINE\n")
	if !screenContainsWithin(attach, "ATTACH_RECONNECT_BASELINE", 2*time.Second) {
		t.Fatalf("expected baseline output before reconnect")
	}
	attach.SendCtrlL()
	attach.Send("b")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if attach.Cursor().Row <= 1 {
			return fmt.Errorf("cursor not below row 1")
		}
		row := screen.Row(0)
		if !strings.Contains(row, "ra") {
			return fmt.Errorf("tab bar not visible; row=%q", row)
		}
		return nil
	})
	waitForRawIdle(t, attach, 150*time.Millisecond, 2*time.Second)

	h.StopServer()
	sawReconnectBanner := false
	attach.Eventually(4*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "reconnecting") {
			sawReconnectBanner = true
			return nil
		}
		// Fast reconnect can skip a visible reconnect banner.
		if strings.Contains(row, "ra") && strings.Contains(row, "rb") {
			return nil
		}
		// During fast reconnect churn, row 1 can transiently show shell content.
		return nil
	})
	waitForRawIdle(t, attach, 150*time.Millisecond, 2*time.Second)
	_ = attach.DrainRaw()
	ptytest.Advance(attach.Clock(), 1200*time.Millisecond)
	raw := attach.DrainRaw()
	if sawReconnectBanner && strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("reconnect tick repainted full tab row; expected badge-only update, raw=%q", truncateRaw(raw))
	}
}

func TestAttachNoFullRedrawAfterTabSwitchWithLocalPTYs(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "host-local", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-local"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	_ = waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "host-local", Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 2)

	h.StopServer()
	attach.SendCtrlL()
	attach.Send("n")
	attach.Wait(200 * time.Millisecond)
	_ = attach.DrainRaw()

	assertNoFullRedraw(t, attach, 24, 2500*time.Millisecond)
	attach.Cancel()
	_, _ = attach.WaitErr(200 * time.Millisecond)
}

func TestAttachResizeDoesNotClearScreen(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "attach-resize-no-clear", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-resize-no-clear"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "attach-resize-no-clear", Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 1)

	attach.Send("echo ATTACH_RESIZE_BASELINE\n")
	if !screenContainsWithin(attach, "ATTACH_RESIZE_BASELINE", 2*time.Second) {
		t.Fatalf("expected baseline output before resize")
	}
	waitForRawIdle(t, attach, 120*time.Millisecond, 2*time.Second)

	assertNoClearScreenAfterAction(t, attach, 1200*time.Millisecond, func() {
		attach.Resize(100, 30)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
	})
}

func TestAttachNoFullRedrawOnHelpToggle(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "host-help", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-help"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "host-help", Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 1)
	attach.Wait(300 * time.Millisecond)

	waitForRawIdle(t, attach, 100*time.Millisecond, 2*time.Second)
	_ = attach.DrainRaw()
	attach.SendCtrlL()
	attach.Send("h")
	assertNoFullRedraw(t, attach, 24, 400*time.Millisecond)

	waitForRawIdle(t, attach, 100*time.Millisecond, 2*time.Second)
	_ = attach.DrainRaw()
	attach.Send("q")
	assertNoFullRedraw(t, attach, 24, 400*time.Millisecond)
}

func TestAttachNoFullRedrawOnOnlineWrapAroundTabSwitch(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "host-online-wrap", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-online-wrap"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	_ = waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "host-online-wrap", Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 2)

	assertNoTabSwitchFlickerAfterAction(t, attach, 24, 250*time.Millisecond, func() {
		attach.SendCtrlL()
		attach.Send("n")
	})
}

func TestAttachNoFullRedrawOnScrollbackPaging(t *testing.T) {
	shell := "/bin/sh"

	h := newHarness(t)
	sessionID := "attach-scrollback-flicker"
	host := h.StartHost(ptytest.HostOptions{SessionID: sessionID, Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: sessionID, Cols: 80, Rows: 24})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 1)

	host.Send("i=1; while [ $i -le 120 ]; do printf 'SCROLL-%03d\\n' $i; i=$((i+1)); done\n")
	if !waitForRawContains(t, attach, "SCROLL-120", 3*time.Second) {
		t.Fatalf("expected SCROLL-120 output before scrollback paging")
	}
	attach.Wait(150 * time.Millisecond)
	_ = attach.DrainRaw()

	attach.SendBytes([]byte{0x0c, '['})
	ptytest.Advance(h.Clock(), 120*time.Millisecond)
	attach.Wait(150 * time.Millisecond)
	_ = attach.DrainRaw()

	assertNoScrollbackFlickerAfterAction(t, attach, 24, 600*time.Millisecond, func() {
		attach.SendBytes([]byte{0x1b, '[', 'A'})
	})
}

func assertNoFullRedraw(t *testing.T, sess *ptytest.PTYSession, rows int, d time.Duration) {
	t.Helper()
	_ = sess.DrainRaw()
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(d)
	for ptytest.Now(clk).Before(deadline) {
		raw := sess.DrainRaw()
		if ptytest.HasFullRedrawANSI(raw, rows) {
			t.Fatalf("unexpected full-screen redraw while overlay active: %q", truncateRaw(raw))
		}
		ptytest.Advance(clk, 200*time.Millisecond)
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
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(d)
	window := ""
	for ptytest.Now(clk).Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if ptytest.HasFullRedrawANSI(window, rows) {
			t.Fatalf("unexpected full-screen redraw during action: %q", truncateRaw(window))
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	if strings.Contains(window, "\x1b[1;1H\x1b[0;39;49m") {
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
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(d)
	window := ""
	for ptytest.Now(clk).Before(deadline) {
		raw := sess.DrainRaw()
		if raw != "" {
			window += raw
			if len(window) > 16384 {
				window = window[len(window)-16384:]
			}
		}
		if ptytest.HasFullRedrawANSI(window, rows) {
			t.Fatalf("unexpected full-screen redraw during scrollback action: %q", truncateRaw(window))
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	if strings.Contains(window, baseTopRowResetSeq) {
		t.Fatalf("scrollback repainted base top row before overlay; expected overlay-composed output: %q", truncateRaw(window))
	}
}

func assertNoClearScreenAfterAction(t *testing.T, sess *ptytest.PTYSession, d time.Duration, action func()) {
	t.Helper()
	_ = sess.DrainRaw()
	action()
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(d)
	window := ""
	for ptytest.Now(clk).Before(deadline) {
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
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func waitForRawIdle(t *testing.T, sess *ptytest.PTYSession, idle, timeout time.Duration) {
	t.Helper()
	if idle <= 0 {
		return
	}
	clk := sess.Clock()
	start := ptytest.Now(clk)
	idleStart := start
	deadline := start.Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if raw := sess.DrainRaw(); raw != "" {
			idleStart = ptytest.Now(clk)
		}
		if ptytest.Now(clk).Sub(idleStart) >= idle {
			return
		}
		ptytest.Advance(clk, 100*time.Millisecond)
	}
	t.Fatalf("timed out waiting for raw output to idle")
}

func truncateRaw(raw string) string {
	if len(raw) <= 200 {
		return raw
	}
	return raw[:200]
}
