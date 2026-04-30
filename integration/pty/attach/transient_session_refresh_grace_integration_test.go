//go:build integration
// +build integration

package integrationptyattach_test

import (
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRetainsTransientlyMissingActiveRemoteSession(t *testing.T) {
	h := newHarness(t)
	alpha := h.StartHost(ptytest.HostOptions{
		SessionID:   "alpha",
		SessionName: "alpha",
	})
	beta := h.StartHost(ptytest.HostOptions{
		SessionID:   "beta",
		SessionName: "beta",
	})
	t.Cleanup(alpha.Cancel)
	t.Cleanup(beta.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"alpha", "beta"})

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := map[string]*attach.Client{}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "alpha",
		RefreshInterval: 100 * time.Millisecond,
		BackoffPolicy: backoff.Policy{
			Base:   100 * time.Millisecond,
			Factor: 1.0,
			Max:    100 * time.Millisecond,
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
	t.Cleanup(attachSess.Cancel)

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	if currentActive != "alpha" {
		t.Fatalf("expected alpha to be active first, got %q", currentActive)
	}

	_ = advanceActiveTabWithRetry(t, attachSess, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)
	activeMu.Lock()
	if activeID != "beta" {
		t.Fatalf("expected beta to be active after switch, got %q", activeID)
	}
	activeMu.Unlock()

	beta.Cancel()
	h.Advance(200 * time.Millisecond)
	activeMu.Lock()
	current := activeID
	activeMu.Unlock()
	if current != "beta" {
		t.Fatalf("expected beta to remain active during transient miss, got %q", current)
	}

	beta = h.StartHost(ptytest.HostOptions{
		SessionID:   "beta",
		SessionName: "beta",
	})
	t.Cleanup(beta.Cancel)
	waitForHost(t, h, "beta", 3*time.Second)
	h.Advance(200 * time.Millisecond)

	waitForClientReady(t, h.Clock(), &viewsMu, views, "beta", 3*time.Second)
	beta.Send("BETA_BACK\n")
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("BETA_BACK") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
	activeMu.Lock()
	current = activeID
	activeMu.Unlock()
	if current != "beta" {
		t.Fatalf("expected beta to remain active after reconnect, got %q", current)
	}
}
