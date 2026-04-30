package attach_test

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func waitForActiveSessionReady(t *testing.T, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		activeMu.Lock()
		current := *active
		activeMu.Unlock()
		if current != "" && (prev == "" || current != prev) {
			if clientReady(viewsMu, views, current) {
				return current
			}
		}
		if fallback := readySessionID(viewsMu, views, prev); fallback != "" {
			return fallback
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	if prev == "" {
		t.Fatalf("timed out waiting for active session ready")
	}
	t.Fatalf("timed out waiting for active session ready after %q", prev)
	return ""
}

func clientReady(viewsMu *sync.Mutex, views map[string]*attach.Client, id string) bool {
	viewsMu.Lock()
	client := views[id]
	viewsMu.Unlock()
	return client != nil && client.Connected()
}

func readySessionID(viewsMu *sync.Mutex, views map[string]*attach.Client, exclude string) string {
	viewsMu.Lock()
	ids := make([]string, 0, len(views))
	for id, client := range views {
		if id == exclude {
			continue
		}
		if client != nil && client.Connected() {
			ids = append(ids, id)
		}
	}
	viewsMu.Unlock()
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func screenContainsWithin(sess *ptytest.PTYSession, token string, timeout time.Duration) bool {
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if strings.Contains(sess.Screen().String(), token) {
			return true
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return strings.Contains(sess.Screen().String(), token)
}

func waitForClientReady(t *testing.T, clk clock.Clock, mu *sync.Mutex, views map[string]*attach.Client, id string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		mu.Lock()
		client := views[id]
		mu.Unlock()
		if client != nil && client.Connected() {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for client %q ready", id)
}

func waitForTabLabels(t *testing.T, sess *ptytest.PTYSession, labels []string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(sess.Clock()).Add(timeout)
	for ptytest.Now(sess.Clock()).Before(deadline) {
		row := sess.Screen().Row(0)
		missing := false
		for _, label := range labels {
			if !strings.Contains(row, label) {
				missing = true
				break
			}
		}
		if !missing {
			return
		}
		ptytest.Advance(sess.Clock(), 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for tab labels %v in row %q", labels, sess.Screen().Row(0))
}

func primeTabsByCount(t *testing.T, sess *ptytest.PTYSession, count int) {
	t.Helper()
	for i := 0; i < count-1; i++ {
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 150*time.Millisecond)
	}
	ptytest.Advance(sess.Clock(), 300*time.Millisecond)
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

func advanceActiveTabWithRetry(t *testing.T, sess *ptytest.PTYSession, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 150*time.Millisecond)
		if next := waitForActiveSessionReadyOptional(clk, activeMu, active, viewsMu, views, prev, 400*time.Millisecond); next != "" {
			return next
		}
	}
	t.Fatalf("timed out waiting for active session ready after %q", prev)
	return ""
}

func waitForActiveSessionReadyOptional(clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		activeMu.Lock()
		current := *active
		activeMu.Unlock()
		if current != "" && current != prev {
			if clientReady(viewsMu, views, current) {
				return current
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return ""
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
