package session_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRemoteTabSwitchAfterRelayDropNoRemoteOutput(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{SessionID: "switchA", SessionName: "switchA", Shell: shell, Cols: 100, Rows: 30})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "switchB", SessionName: "switchB", Shell: shell, Cols: 100, Rows: 30})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 4, 6*time.Second)

	hostB.Send("sleep 5\n")
	primeTabsByCountSession(t, hostA, 4)

	h.StopServer()
	h.Advance(400 * time.Millisecond)
	h.RestartServer()
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 4, 6*time.Second)

	hostA.Send("\n")
	hostA.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		cur := hostA.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("expected cursor below top row before tab switch; got row %d col %d", cur.Row, cur.Col)
		}
		if strings.TrimSpace(screen.Row(0)) == "" {
			return fmt.Errorf("expected tab bar visible before tab switch")
		}
		return nil
	})

	beforeTopRow := hostA.Screen().Row(0)
	hostA.DrainRaw()
	traceOffset := traceSize(t, h.TracePath())
	hostA.SendCtrlL()
	hostA.Send("n")
	hostA.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !hasTabSwitchEventSince(h.TracePath(), traceOffset) && screen.Row(0) == beforeTopRow {
			return fmt.Errorf("tab bar did not update without remote output")
		}
		return nil
	})
}
