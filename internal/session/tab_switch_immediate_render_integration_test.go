package session_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestRemoteTabSwitchRendersWithoutExtraInput(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID: "host-b",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID: "host-a",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.Send("echo LOCAL_MARK\n")
	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("LOCAL_MARK") {
			return fmt.Errorf("expected LOCAL_MARK on host A")
		}
		return nil
	})

	hostB.Send("echo REMOTE_MARK\n")
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("REMOTE_MARK") {
			return fmt.Errorf("expected REMOTE_MARK on host B")
		}
		return nil
	})

	// Build both tabs and return to local tab so remote tab is hidden but ready.
	primeTabsByCountSession(t, hostA, 2)
	hostA.SendCtrlL()
	hostA.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	// Switch to remote tab. No extra input should be required for redraw.
	hostA.SendCtrlL()
	hostA.Send("n")
	if !screenContainsWithin(hostA, "REMOTE_MARK", 1500*time.Millisecond) {
		t.Fatalf("expected remote snapshot after tab switch without extra input")
	}
}
