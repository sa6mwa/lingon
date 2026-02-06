package session_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostAltScreenInitialRender(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-initial",
		SessionName: "host-initial",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	host.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !screen.Contains("host-initial") && row == "" && !screen.Contains("PROMPT>") {
			return fmt.Errorf("expected tab bar or prompt rendered, got blank top row")
		}
		return nil
	})

	host.Send("echo READY\n")
	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("READY") {
			return fmt.Errorf("expected READY output")
		}
		return nil
	})
}
