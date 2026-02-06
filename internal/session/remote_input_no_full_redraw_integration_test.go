package session_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestRemoteInputDoesNotFullRedraw(t *testing.T) {
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

	hostA.Send("echo READY_A\n")
	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("READY_A") {
			return fmt.Errorf("expected READY_A output on host A")
		}
		return nil
	})
	hostB.Send("echo READY_B\n")
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("READY_B") {
			return fmt.Errorf("expected READY_B output on host B")
		}
		return nil
	})

	primeTabsByCountSession(t, hostA, 2)

	if !switchHostToRemoteByEcho(hostA, hostB, "REMOTE_OK", 4) {
		t.Fatalf("expected REMOTE_OK output on host B")
	}

	_ = hostA.DrainRaw()
	hostA.SendBytes([]byte{0x7f})
	assertNoFullRedraw(t, hostA, 24, 500*time.Millisecond)
}

func switchHostToRemoteByEcho(from, to *ptytest.PTYSession, token string, attempts int) bool {
	for i := 0; i < attempts; i++ {
		from.SendCtrlL()
		from.Send("n")
		advanceTestClock(from.Clock(), 150*time.Millisecond)
		from.Send("echo " + token + "\n")
		if screenContainsWithin(to, token, 500*time.Millisecond) {
			return true
		}
	}
	return false
}
