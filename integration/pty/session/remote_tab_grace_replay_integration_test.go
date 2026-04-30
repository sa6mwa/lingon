//go:build integration
// +build integration

package integrationptysession_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRemoteTransientMissingTabRetainedAndReplayed(t *testing.T) {
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

	const token = "REMOTE_TAB_CACHE_TOKEN"
	hostB.Send("echo " + token + "\n")
	if !switchToToken(hostA, token, 4, 600*time.Millisecond) {
		t.Fatalf("unable to switch hostA to hostB tab")
	}
	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(token) {
			return ptytest.FormatRowDiff("hostA", 0, screen.Row(0))
		}
		return nil
	})

	hostB.Cancel()
	advanceTestClock(h.Clock(), 1*time.Second)

	hostA.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(token) {
			return ptytest.FormatRowDiff("hostA", 0, screen.Row(0))
		}
		return nil
	})

	hostB = h.StartHost(ptytest.HostOptions{
		SessionID:   "hostB",
		SessionName: "hostB",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	const readyAfter = "REMOTE_TAB_REPLAY_READY_AFTER"
	hostB.Send("echo " + readyAfter + "\n")
	if !screenContainsWithin(hostB, readyAfter, 8*time.Second) {
		t.Fatalf("expected hostB to accept input after reconnect")
	}
	if !switchToToken(hostA, readyAfter, 4, 600*time.Millisecond) {
		t.Fatalf("expected hostA to switch back to retained hostB tab after reconnect")
	}
	if !screenContainsWithin(hostA, readyAfter, 8*time.Second) {
		t.Fatalf("expected hostA remote tab to replay retained session after reconnect")
	}

	const tokenAfter = "REMOTE_TAB_REPLAY_TOKEN_AFTER"
	hostB.Send("echo " + tokenAfter + "\n")
	if !screenContainsWithin(hostB, tokenAfter, 3*time.Second) {
		t.Fatalf("expected replay source to echo %q after reconnect", tokenAfter)
	}
}
