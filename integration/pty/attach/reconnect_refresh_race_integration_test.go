//go:build integration

package integrationptyattach_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/ptytest"
)

func TestReconnectViewConstructionWithConcurrentSessionRefresh(t *testing.T) {
	h := newHarness(t)
	const id = "reconnect-refresh"
	host := h.StartHost(ptytest.HostOptions{SessionID: id, Shell: writeAttachPromptShell(t)})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{id})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	gate := netgate.New(h.Clock())
	var reconnecting atomic.Bool
	constructing := make(chan struct{})
	release := make(chan struct{})
	ready := make(chan *attach.Client, 4)
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: id,
		Gate:      gate,
		// No elapsed-time wait is involved: OnReconnect opens the mock gate.
		BackoffPolicy: backoff.Policy{Base: time.Nanosecond, Max: time.Nanosecond, Factor: 1},
		OnReconnect: func(string, int) {
			gate.Allow()
			reconnecting.Store(true)
		},
		TokenRefresher: func(callCtx context.Context) (string, error) {
			if reconnecting.CompareAndSwap(true, false) {
				close(constructing)
				select {
				case <-release:
				case <-callCtx.Done():
					return "", callCtx.Err()
				}
			}
			return h.AccessToken(), nil
		},
		OnView: func(_ string, client *attach.Client) {
			onReady := client.OnReady
			client.OnReady = func() {
				onReady()
				select {
				case ready <- client:
				case <-ctx.Done():
				}
			}
		},
	})
	t.Cleanup(sess.Cancel)
	waitReady := func() *attach.Client {
		t.Helper()
		select {
		case client := <-ready:
			return client
		case <-ctx.Done():
			t.Fatalf("waiting for attach readiness: %v", ctx.Err())
			return nil
		}
	}
	first := waitReady()
	first.Close("reconnect with concurrent session refresh")
	select {
	case <-constructing:
	case <-ctx.Done():
		t.Fatalf("waiting for reconnect construction: %v", ctx.Err())
	}
	refreshed := make(chan struct{})
	refreshedWhilePreparing := make(chan struct{})
	go func() {
		defer close(refreshed)
		// Session updates must continue while credential refresh is paused;
		// protecting the copy must not hold the UI mutex across network work.
		first.OnSessions([]attach.SessionInfo{{ID: id, Name: id, Status: "running"}})
		close(refreshedWhilePreparing)
		<-release
		// Refreshes and construction start from the same barrier. With the
		// shared view lock, each complete refresh is ordered with the copy.
		for range 1000 {
			first.OnSessions([]attach.SessionInfo{{ID: id, Name: id, Status: "running"}})
		}
	}()
	select {
	case <-refreshedWhilePreparing:
	case <-ctx.Done():
		t.Fatalf("credential refresh blocked session updates: %v", ctx.Err())
	}
	close(release)
	select {
	case <-refreshed:
	case <-ctx.Done():
		t.Fatalf("session refresh blocked reconnect: %v", ctx.Err())
	}
	if next := waitReady(); next == first {
		t.Fatal("reconnect did not create a replacement view")
	}
	sess.Send("printf 'REFRESH_%s\\n' RECONNECTED\r")
	if err := sess.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
		return screen.Contains("REFRESH_RECONNECTED")
	}); err != nil {
		t.Fatalf("reconnected terminal did not accept input: %v", err)
	}
}
