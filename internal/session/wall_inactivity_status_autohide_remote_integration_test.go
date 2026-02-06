package session_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestWallInactivityStatusAutoHideOnRemoteTab(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-a",
		SessionName: "host-a",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-b",
		SessionName: "host-b",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	remoteMark := "REMOTE_WALL_INACTIVITY_MARK"
	hostB.Send("echo " + remoteMark + "\n")
	if !screenContainsWithin(hostB, remoteMark, 2*time.Second) {
		t.Fatalf("expected remote marker on host-b")
	}

	primeTabsByCountSession(t, hostA, 2)
	if !switchToToken(hostA, remoteMark, 4, 700*time.Millisecond) {
		t.Fatalf("unable to switch host-a to remote tab")
	}

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 2m", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner")
	}

	advanceTestClock(h.Clock(), 3*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 100*time.Millisecond, func() error {
		if hostA.Screen().Contains("wall inactivity 2m") {
			return errStillVisible("wall inactivity status still visible after timeout")
		}
		return nil
	})
}

type stillVisibleErr string

func (e stillVisibleErr) Error() string { return string(e) }

func errStillVisible(msg string) error { return stillVisibleErr(msg) }
