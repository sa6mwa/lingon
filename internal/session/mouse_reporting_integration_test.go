package session_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostStartupDoesNotEnableMouseReporting(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "mouse-host",
		SessionName: "mouse-host",
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})

	waitForHost(t, h, "mouse-host", 3*time.Second)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	raw := host.DrainRaw()
	if strings.Contains(raw, "\x1b[?1000h") || strings.Contains(raw, "\x1b[?1006h") {
		t.Fatalf("host startup enabled mouse reporting; raw=%q", truncateRaw(raw))
	}
}
