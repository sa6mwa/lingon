package attach_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachDoesNotHammerAPIOnClosedLocalPTY(t *testing.T) {
	var sessionsCount int64
	var wsClientCount int64

	h := newHarness(t, ptytest.WithRequestHook(func(r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			atomic.AddInt64(&sessionsCount, 1)
		case strings.HasSuffix(r.URL.Path, "/ws/client"):
			atomic.AddInt64(&wsClientCount, 1)
		}
	}))

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/cat"); err == nil {
		shell = "/bin/cat"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host",
		SessionName: "host",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host"})

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host",
		Cols:      120,
		Rows:      30,
		OnActive: func(sessionID string) {
			activeMu.Lock()
			activeID = sessionID
			activeMu.Unlock()
		},
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	host.SendBytes([]byte{0x0c, 'c'})
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)
	newSessionID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host", 3*time.Second)

	attach.SendCtrlL()
	attach.Send("n")
	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	if currentActive != newSessionID {
		currentActive = waitForActiveSessionReadyOptional(h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)
	}
	if currentActive != newSessionID {
		t.Fatalf("expected active session %q after switch, got %q", newSessionID, currentActive)
	}
	waitForClientReady(t, h.Clock(), &viewsMu, views, currentActive, 5*time.Second)

	matched := false
	for i := 0; i < 3; i++ {
		token := fmt.Sprintf("HOST_SYNC_%d", i)
		host.Send(token + "\n")
		waitForRawIdleAfterOutput(t, attach, 100*time.Millisecond, 2*time.Second)
		if screenContainsWithin(attach, token, 500*time.Millisecond) {
			matched = true
			break
		}
		host.SendBytes([]byte{0x0c, 'n'})
	}
	if !matched {
		t.Fatalf("unable to align host with attach session before close")
	}
	host.SendBytes([]byte{0x0c, 'Q'})
	waitForSessionRemoval(t, h.Clock(), h.Endpoint(), h.AccessToken(), newSessionID, 5*time.Second)
	startSessions, startWS := waitForRequestCountersStable(t, h.Clock(), &sessionsCount, &wsClientCount, 800*time.Millisecond, 5*time.Second)

	h.Advance(2 * time.Second)

	deltaSessions := atomic.LoadInt64(&sessionsCount) - startSessions
	deltaWS := atomic.LoadInt64(&wsClientCount) - startWS

	if deltaSessions > 3 || deltaWS > 3 {
		t.Fatalf("unexpected request churn after session close: sessions=%d ws=%d", deltaSessions, deltaWS)
	}
	_ = attach
}

func screenContainsWithin(sess *ptytest.PTYSession, token string, timeout time.Duration) bool {
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if sess.Screen().Contains(token) {
			return true
		}
		ptytest.Advance(clk, 25*time.Millisecond)
	}
	return sess.Screen().Contains(token)
}

func waitForRawIdleAfterOutput(t *testing.T, sess *ptytest.PTYSession, idle, timeout time.Duration) {
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

func waitForSessionRemoval(t *testing.T, clk clock.Clock, endpoint, token, id string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		found, err := fetchSessionIDs(endpoint, token)
		if err == nil && !found[id] {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for session removal: %s", id)
}

func waitForRequestCountersStable(t *testing.T, clk clock.Clock, sessionsCount, wsClientCount *int64, stableFor, timeout time.Duration) (int64, int64) {
	t.Helper()
	lastSessions := atomic.LoadInt64(sessionsCount)
	lastWS := atomic.LoadInt64(wsClientCount)
	stableSince := ptytest.Now(clk)
	deadline := stableSince.Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		ptytest.Advance(clk, 100*time.Millisecond)
		currentSessions := atomic.LoadInt64(sessionsCount)
		currentWS := atomic.LoadInt64(wsClientCount)
		if currentSessions != lastSessions || currentWS != lastWS {
			lastSessions = currentSessions
			lastWS = currentWS
			stableSince = ptytest.Now(clk)
			continue
		}
		if ptytest.Now(clk).Sub(stableSince) >= stableFor {
			return lastSessions, lastWS
		}
	}
	t.Fatalf("timed out waiting for request counters to stabilize")
	return 0, 0
}
