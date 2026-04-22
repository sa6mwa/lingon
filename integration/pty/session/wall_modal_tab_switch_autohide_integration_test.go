//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

func TestWallModalAutoHideAfterSwitchToLocalWithoutInput(t *testing.T) {
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

	remoteMark := "REMOTE_WALL_MARK"
	hostB.Send("echo " + remoteMark + "\n")
	if !screenContainsWithin(hostB, remoteMark, 2*time.Second) {
		t.Fatalf("expected remote marker on host-b")
	}

	primeTabsByCountSession(t, hostA, 2)
	if !switchToToken(hostA, remoteMark, 4, 700*time.Millisecond) {
		t.Fatalf("unable to switch host-a to remote tab")
	}

	wallMsg := "WALL_TIMEOUT_MARK"
	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	assertNoFullRedrawAfterAction(t, hostA, 30, 800*time.Millisecond, func() {
		if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), wallMsg, tlsDir, false); err != nil {
			t.Fatalf("send wall: %v", err)
		}
	})
	if !screenContainsWithin(hostA, wallMsg, 2*time.Second) {
		t.Fatalf("expected wall modal on remote tab")
	}

	hostA.SendCtrlL()
	hostA.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	localMark := "LOCAL_SWITCH_MARK"
	hostA.Send("echo " + localMark + "\n")
	if !screenContainsWithin(hostA, localMark, 2*time.Second) {
		t.Fatalf("expected local marker after tab switch")
	}
	if screenContainsWithin(hostB, localMark, 1200*time.Millisecond) {
		t.Fatalf("host-a input still routed to remote tab after switch")
	}

	assertNoFullRedrawAfterAction(t, hostA, 30, 800*time.Millisecond, func() {
		advanceTestClock(h.Clock(), 6*time.Second)
	})
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 100*time.Millisecond, func() error {
		if hostA.Screen().Contains(wallMsg) {
			return fmt.Errorf("wall modal still visible after timeout without input")
		}
		return nil
	})
}
