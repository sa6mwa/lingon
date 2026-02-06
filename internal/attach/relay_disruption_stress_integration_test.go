package attach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRelayDisruptionStress(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{SessionID: "stressA", SessionName: "stressA", Shell: shell, Cols: 120, Rows: 30})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "stressB", SessionName: "stressB", Shell: shell, Cols: 120, Rows: 30})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 6*time.Second)

	attachA, activeA, viewsA := startTrackedAttach(t, h, "stressA")
	attachB, activeB, viewsB := startTrackedAttach(t, h, "stressB")
	t.Cleanup(attachA.Cancel)
	t.Cleanup(attachB.Cancel)

	primeTabsByCountWithActive(t, attachA, 4, h.Clock(), activeA)
	primeTabsByCountWithActive(t, attachB, 4, h.Clock(), activeB)

	cycles := 2
	activeReadyTimeout := 6 * time.Second
	for i := 0; i < cycles; i++ {
		tokensA := cycleSendTokensWithActive(t, attachA, 1, "STRESS_A", h.Clock(), activeA.mu, activeA.id, activeA.viewsMu, viewsA)
		tokensB := cycleSendTokensWithActive(t, attachB, 1, "STRESS_B", h.Clock(), activeB.mu, activeB.id, activeB.viewsMu, viewsB)
		assertTokensVisibleAcrossTabs(t, attachA, 4, tokensA, "attachA")
		assertTokensVisibleAcrossTabs(t, attachB, 4, tokensB, "attachB")

		h.StopServer()
		h.Advance(400 * time.Millisecond)
		h.RestartServer()
		waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 6*time.Second)
		_ = waitForActiveSessionReady(t, h.Clock(), activeA.mu, activeA.id, activeA.viewsMu, viewsA, "", activeReadyTimeout)
		_ = waitForActiveSessionReady(t, h.Clock(), activeB.mu, activeB.id, activeB.viewsMu, viewsB, "", activeReadyTimeout)

		primeTabsByCountWithActive(t, attachA, 4, h.Clock(), activeA)
		primeTabsByCountWithActive(t, attachB, 4, h.Clock(), activeB)
	}
}

type activeTracker struct {
	mu      *sync.Mutex
	viewsMu *sync.Mutex
	id      *string
	views   map[string]*attach.Client
}

func startTrackedAttach(t *testing.T, h *ptytest.Harness, sessionID string) (*ptytest.PTYSession, *activeTracker, map[string]*attach.Client) {
	t.Helper()
	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	tracker := &activeTracker{
		mu:      &activeMu,
		viewsMu: &viewsMu,
		id:      &activeID,
		views:   views,
	}
	return attachSess, tracker, views
}

func primeTabsByCountWithActive(t *testing.T, sess *ptytest.PTYSession, count int, clk clock.Clock, tracker *activeTracker) {
	t.Helper()
	current := waitForActiveSessionReady(t, clk, tracker.mu, tracker.id, tracker.viewsMu, tracker.views, "", 3*time.Second)
	for i := 0; i < count-1; i++ {
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 150*time.Millisecond)
		current = waitForActiveSessionReadyOptional(clk, tracker.mu, tracker.id, tracker.viewsMu, tracker.views, current, 3*time.Second)
	}
	_ = current
}

func waitForActiveSessionReadyOptional(clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		activeMu.Lock()
		current := *active
		activeMu.Unlock()
		if current != "" && (prev == "" || current != prev) {
			viewsMu.Lock()
			client := views[current]
			viewsMu.Unlock()
			if client != nil && client.Connected() && client.Snapshot() != nil {
				return current
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return prev
}
