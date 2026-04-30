//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachWaitsForSessionsAfterRelayReconnect(t *testing.T) {
	h := newHarness(t)
	sessionID := "session_wait_reconnect"
	host := h.StartHost(ptytest.HostOptions{SessionID: sessionID})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	var activeMu sync.Mutex
	var activeID string
	var viewsMu sync.Mutex
	views := map[string]*attach.Client{}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       sessionID,
		RefreshInterval: 200 * time.Millisecond,
		BackoffPolicy: backoff.Policy{
			Base:   100 * time.Millisecond,
			Factor: 1.0,
			Max:    200 * time.Millisecond,
		},
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
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	h.StopServer()
	host.Cancel()
	h.RestartServer()

	if screenContainsWithin(attachSess, "no sessions available", 200*time.Millisecond) {
		t.Fatalf("unexpected exit on empty sessions after reconnect")
	}

	host = h.StartHost(ptytest.HostOptions{SessionID: sessionID})
	waitForHost(t, h, sessionID, 5*time.Second)

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	host.Send("HOST_RECONNECTED\n")
	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Waiting for sessions") || screen.Contains("Not connected") {
			return fmt.Errorf("expected disconnect overlay cleared")
		}
		if !screen.Contains("HOST_RECONNECTED") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
}
