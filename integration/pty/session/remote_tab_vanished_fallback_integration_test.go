//go:build integration
// +build integration

package integrationptysession_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostFallsBackWhenPreferredRemoteTabVanishes(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "hostA",
		SessionName: "hostA",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "hostB",
		SessionName: "hostB",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	const remoteToken = "REMOTE_VANISH_TOKEN"
	hostB.Send("echo " + remoteToken + "\n")
	if !switchToToken(hostA, remoteToken, 4, 600*time.Millisecond) {
		t.Fatalf("unable to switch hostA to hostB tab")
	}
	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(remoteToken) {
			return ptytest.FormatRowDiff("hostA", 0, screen.Row(0))
		}
		return nil
	})

	hostB.Cancel()
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 8*time.Second)
	advanceTestClock(h.Clock(), 70*time.Second)

	const localToken = "LOCAL_FALLBACK_AFTER_REMOTE_EXIT"
	hostA.Send("echo " + localToken + "\n")
	if !screenContainsWithin(hostA, localToken, 3*time.Second) {
		t.Fatalf("expected hostA to fall back to a live tab after remote tab vanished")
	}
}
