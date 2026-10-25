//go:build integration
// +build integration

package integrationptyattach_test

import (
	"context"
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
	activeEvents := make(chan string, 8)
	closedBeta := make(chan struct{}, 1)
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
			changed := activeID != sessionID
			activeID = sessionID
			activeMu.Unlock()
			if changed {
				activeEvents <- sessionID
			}
		},
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
		OnViewClosed: func(sessionID string, _, _ bool) {
			if sessionID == "beta" {
				select {
				case closedBeta <- struct{}{}:
				default:
				}
			}
		},
	})
	t.Cleanup(attachSess.Cancel)

	waitActive := func(want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case got := <-activeEvents:
			if got != want {
				t.Fatalf("expected active session %q, got %q", want, got)
			}
		case <-ctx.Done():
			t.Fatalf("waiting for active session %q: %v", want, ctx.Err())
		}
	}
	waitScreen := func(token string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := attachSess.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains(token) }); err != nil {
			t.Fatalf("waiting for rendered %q: %v", token, err)
		}
	}
	alpha.Send("printf 'ALPHA_%s\\n' READY\r")
	beta.Send("printf 'BETA_%s\\n' READY\r")
	waitActive("alpha")
	waitScreen("ALPHA_READY")
	// Send exactly one switch. Retrying a non-idempotent key command while
	// waiting for its snapshot can queue a second switch back to alpha.
	attachSess.SendBytes([]byte{12, 'n'})
	waitActive("beta")
	waitScreen("BETA_READY")

	beta.Cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	select {
	case <-closedBeta:
	case <-ctx.Done():
		t.Fatalf("waiting for beta disconnect: %v", ctx.Err())
	}
	viewsMu.Lock()
	betaClient := views["beta"]
	viewsMu.Unlock()
	// Deliver the authoritative missing-session event through the same
	// callback as the relay, before advancing the grace interval.
	missing := func() { betaClient.OnSessions([]attach.SessionInfo{{ID: "alpha", Name: "alpha", Status: "running"}}) }
	missing()
	h.Advance(200 * time.Millisecond)
	missing()
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

	beta.Send("printf 'BETA_%s\\n' BACK\r")
	waitScreen("BETA_BACK")
	activeMu.Lock()
	current = activeID
	activeMu.Unlock()
	if current != "beta" {
		t.Fatalf("expected beta to remain active after reconnect, got %q", current)
	}
}
