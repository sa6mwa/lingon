package session_test

import (
	"fmt"
	"os"
	"strings"
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

func TestSwitchToDisconnectedRemoteTabDoesNotShowWallInactivityOff(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-wall-switch-a",
		SessionName: "host-wall-switch-a",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-wall-switch-b",
		SessionName: "host-wall-switch-b",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	localMark := "LOCAL_WALL_SWITCH_MARK"
	hostA.Send("echo " + localMark + "\n")
	if !screenContainsWithin(hostA, localMark, 2*time.Second) {
		t.Fatalf("expected local marker on host-a")
	}

	remoteMark := "REMOTE_WALL_SWITCH_MARK"
	hostB.Send("echo " + remoteMark + "\n")
	if !screenContainsWithin(hostB, remoteMark, 2*time.Second) {
		t.Fatalf("expected remote marker on host-b")
	}

	primeTabsByCountSession(t, hostA, 2)
	if !switchToToken(hostA, remoteMark, 4, 700*time.Millisecond) {
		t.Fatalf("unable to switch host-a to remote tab")
	}
	hostA.SendCtrlL()
	hostA.Send("p")
	if !screenContainsWithin(hostA, localMark, 2*time.Second) {
		t.Fatalf("expected host-a to return to local tab before disconnect")
	}

	hostB.Cancel()
	advanceTestClock(h.Clock(), 1*time.Second)

	hostA.SendCtrlL()
	hostA.Send("n")
	hostA.Eventually(3*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.Row(0), "wall inactivity off") {
			return fmt.Errorf("unexpected wall inactivity banner while switching to disconnected remote tab: %q", screen.Row(0))
		}
		return nil
	})
}

type stillVisibleErr string

func (e stillVisibleErr) Error() string { return string(e) }

func errStillVisible(msg string) error { return stillVisibleErr(msg) }
