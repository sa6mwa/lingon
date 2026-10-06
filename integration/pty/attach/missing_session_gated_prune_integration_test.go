//go:build integration

package integrationptyattach_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMissingSessionGraceExpiresWhileNetworkGateIsBlocked(t *testing.T) {
	h := newHarness(t)
	gate := netgate.New(h.Clock())
	const first = "gated-prune-a"
	const second = "gated-prune-b"
	a := h.StartHost(ptytest.HostOptions{SessionID: first, Shell: "/bin/sh"})
	b := h.StartHost(ptytest.HostOptions{SessionID: second, Shell: "/bin/sh"})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{first, second})
	a.Send("printf 'SURVIVOR_%s\\n' READY\r")
	b.Send("printf 'MISSING_%s\\n' READY\r")
	var captured atomic.Pointer[attach.Client]
	client := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: first, Gate: gate,
		OnView: func(id string, c *attach.Client) {
			if id == second {
				captured.Store(c)
			}
		},
	})
	client.Eventually(3*time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SURVIVOR_READY") {
			return fmt.Errorf("first snapshot not ready: %s", screen.String())
		}
		return nil
	})
	client.SendBytes([]byte{12, 'n'})
	client.Eventually(3*time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("MISSING_READY") || captured.Load() == nil {
			return fmt.Errorf("second snapshot not ready: %s", screen.String())
		}
		return nil
	})
	// Inject the authoritative discovery event at the same receiver callback
	// used by the real relay frame. The existing first view stays connected.
	gate.BlockFor(time.Hour)
	captured.Load().OnSessions([]attach.SessionInfo{{ID: first, Name: first, Status: "running"}})
	h.Advance(5*time.Second + time.Millisecond)
	client.Eventually(time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SURVIVOR_READY") || screen.Contains(second) {
			return fmt.Errorf("missing tab retained behind network backoff: %s", screen.String())
		}
		return nil
	})
	client.Send("printf 'SURVIVOR_%s\\n' AFTER_PRUNE\r")
	client.Eventually(time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SURVIVOR_AFTER_PRUNE") {
			return fmt.Errorf("surviving view cannot accept input: %s", screen.String())
		}
		return nil
	})
}
