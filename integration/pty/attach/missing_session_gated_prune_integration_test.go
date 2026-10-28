//go:build integration

package integrationptyattach_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMissingSessionGraceExpiresWhileNetworkGateIsBlocked(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	gate := netgate.New(h.Clock())
	const first = "gated-prune-a"
	const second = "gated-prune-b"
	a := h.StartHost(ptytest.HostOptions{SessionID: first, Shell: "/bin/sh"})
	b := h.StartHost(ptytest.HostOptions{SessionID: second, Shell: "/bin/sh"})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{first, second})
	a.Send("printf 'SURVIVOR_%s\\n' READY\r")
	b.Send("printf 'MISSING_%s\\n' READY\r")
	var captured atomic.Pointer[attach.Client]
	var discoveryMu sync.Mutex
	missing := false
	client := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: first, Gate: gate,
		OnView: func(id string, c *attach.Client) {
			onSessions := c.OnSessions
			c.OnSessions = func(updated []attach.SessionInfo) {
				discoveryMu.Lock()
				defer discoveryMu.Unlock()
				if missing {
					filtered := make([]attach.SessionInfo, 0, len(updated))
					for _, session := range updated {
						if session.ID != second {
							filtered = append(filtered, session)
						}
					}
					updated = filtered
				}
				onSessions(updated)
			}
			if id == second {
				captured.Store(c)
			}
		},
	})
	if err := client.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains("SURVIVOR_READY") }); err != nil {
		t.Fatalf("first snapshot not ready: %v", err)
	}
	client.SendBytes([]byte{12, 'n'})
	if err := client.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains("MISSING_READY") && captured.Load() != nil }); err != nil {
		t.Fatalf("second snapshot not ready: %v", err)
	}
	// Inject the authoritative discovery event at the same receiver callback
	// used by the real relay frame. The existing first view stays connected.
	// Keep this fixture's discovery state authoritative on every connection:
	// a late welcome list must not contradict the simulated missing session.
	discoveryMu.Lock()
	missing = true
	discoveryMu.Unlock()
	gate.BlockFor(time.Hour)
	captured.Load().OnSessions([]attach.SessionInfo{{ID: first, Name: first, Status: "running"}})
	h.Advance(5*time.Second - time.Millisecond)
	if !client.Screen().Contains(second) {
		t.Fatal("missing tab pruned before its grace deadline")
	}
	h.Advance(time.Millisecond)
	if err := client.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
		return screen.Contains("SURVIVOR_READY") && !screen.Contains(second)
	}); err != nil {
		t.Fatalf("missing tab retained behind network backoff: %v\n%s", err, client.Screen().String())
	}
	client.Send("printf 'SURVIVOR_%s\\n' AFTER_PRUNE\r")
	if err := client.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains("SURVIVOR_AFTER_PRUNE") }); err != nil {
		t.Fatalf("surviving view cannot accept input: %v", err)
	}
}
