package attach_test

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachSwitchToNewLocalPTYSession(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/cat"); err == nil {
		shell = "/bin/cat"
	}

	h := newHarness(t)
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
	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	host.Send("HOST_READY\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("HOST_READY") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, 'c'})
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)
	newSessionID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host", 3*time.Second)

	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !containsAll(row, []string{"host", "host-2"}) {
			return fmt.Errorf("expected host and host-2 tabs, got %q", row)
		}
		return nil
	})

	attachIsSecond := false
	switchAttachTo := func(second bool, target string) {
		if attachIsSecond == second {
			return
		}
		currentActive = advanceActiveTabWithRetry(t, attach, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 2*time.Second)
		activeMu.Lock()
		activeNow := activeID
		activeMu.Unlock()
		if activeNow != target {
			t.Fatalf("expected active session %q, got %q", target, activeNow)
		}
		attachIsSecond = second
	}

	switchAttachTo(true, newSessionID)
	waitForClientReady(t, h.Clock(), &viewsMu, views, newSessionID, 3*time.Second)
	attach.Send("PING_A1\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("PING_A1") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
	if !tokenVisibleAcrossTabs(host, "PING_A1", 2, 3*time.Second) {
		t.Fatalf("host did not show %q across local tabs", "PING_A1")
	}
	switchAttachTo(false, "host")
	attach.Send("PING_B1\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("PING_B1") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
	if !tokenVisibleAcrossTabs(host, "PING_B1", 2, 3*time.Second) {
		t.Fatalf("host did not show %q across local tabs", "PING_B1")
	}
	switchAttachTo(true, newSessionID)
	attach.Send("PING_A2\n")
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("PING_A2") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
	if !tokenVisibleAcrossTabs(host, "PING_A2", 2, 3*time.Second) {
		t.Fatalf("host did not show %q across local tabs", "PING_A2")
	}
}

func waitForSessionCount(t *testing.T, clk clock.Clock, endpoint, token string, want int, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		found, err := fetchSessionIDs(endpoint, token)
		if err == nil && len(found) >= want {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d sessions", want)
}

func containsAll(row string, labels []string) bool {
	for _, label := range labels {
		if !strings.Contains(row, label) {
			return false
		}
	}
	return true
}

func waitForNewSessionID(t *testing.T, clk clock.Clock, endpoint, token, existing string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		ids, err := fetchSessionIDs(endpoint, token)
		if err == nil {
			for id := range ids {
				if id != existing {
					return id
				}
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for new session id (existing %q)", existing)
	return ""
}

func waitForClientReady(t *testing.T, clk clock.Clock, mu *sync.Mutex, views map[string]*attach.Client, id string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		mu.Lock()
		client := views[id]
		mu.Unlock()
		if client != nil && client.Connected() && client.Snapshot() != nil {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for client ready for %q", id)
}

func tokenVisibleAcrossTabs(sess *ptytest.PTYSession, token string, tabs int, timeout time.Duration) bool {
	clk := sess.Clock()
	if tabs < 1 {
		tabs = 1
	}
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		for i := 0; i < tabs; i++ {
			if sess.Screen().Contains(token) {
				return true
			}
			sess.SendCtrlL()
			sess.Send("n")
			ptytest.Advance(clk, 300*time.Millisecond)
		}
	}
	return sess.Screen().Contains(token)
}
