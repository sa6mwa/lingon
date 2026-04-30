//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRemoteScrollbackShowsIndicatorImmediately(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID: "remote-scrollback-b",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID: "remote-scrollback-a",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	const localMark = "LOCAL_SCROLLBACK_MARK"
	hostA.Send("echo " + localMark + "\n")
	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(localMark) {
			return fmt.Errorf("expected local marker")
		}
		return nil
	})

	const remoteMark = "REMOTE_SCROLLBACK_MARK"
	hostB.Send("echo " + remoteMark + "\n")
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(remoteMark) {
			return fmt.Errorf("expected remote marker on host B")
		}
		return nil
	})

	primeTabsByCountSession(t, hostA, 2)
	hostA.SendCtrlL()
	hostA.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	hostA.SendCtrlL()
	hostA.Send("n")
	if !screenContainsWithin(hostA, remoteMark, 1500*time.Millisecond) {
		t.Fatalf("expected remote tab visible before entering scrollback")
	}

	hostA.SendBytes([]byte{0x0c, '['})
	eventuallyWithClock(t, hostA.Clock(), 1200*time.Millisecond, 50*time.Millisecond, func() error {
		row := hostA.Screen().Row(0)
		if _, ok := scrollbackPercent(row); !ok {
			return fmt.Errorf("expected scrollback indicator on remote tab, got row=%q", row)
		}
		if strings.Contains(row, "remote-scrollback-a") || strings.Contains(row, "remote-scrollback-b") {
			return fmt.Errorf("expected tab bar hidden in remote scrollback, got row=%q", row)
		}
		return nil
	})
}
