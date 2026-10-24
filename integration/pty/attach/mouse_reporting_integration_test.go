//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachStartupDoesNotEnableMouseReporting(t *testing.T) {
	h, sessionID := startMouseReportingHost(t)

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	ptytest.Advance(h.Clock(), 200*time.Millisecond)
	raw := attach.DrainRaw()
	if strings.Contains(raw, "\x1b[?1000h") || strings.Contains(raw, "\x1b[?1006h") {
		t.Fatalf("attach startup enabled mouse reporting; raw=%q", truncateRaw(raw))
	}
}

func TestMultiAttachStartupDoesNotEnableMouseReporting(t *testing.T) {
	h, sessionID := startMouseReportingHost(t)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	ptytest.Advance(h.Clock(), 200*time.Millisecond)
	raw := attach.DrainRaw()
	if strings.Contains(raw, "\x1b[?1000h") || strings.Contains(raw, "\x1b[?1006h") {
		t.Fatalf("multi attach startup enabled mouse reporting; raw=%q", truncateRaw(raw))
	}
}

func TestAttachScrollbackScopesMouseReporting(t *testing.T) {
	h, sessionID := startMouseReportingHost(t)

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	ptytest.Advance(h.Clock(), 200*time.Millisecond)
	_ = attach.DrainRaw()

	attach.SendBytes([]byte{0x0c, '['})
	if !waitForRawContains(t, attach, "\x1b[?1000h\x1b[?1006h", 2*time.Second) {
		t.Fatalf("entering attach scrollback did not enable mouse reporting")
	}

	attach.SendBytes([]byte{'q'})
	if !waitForRawContains(t, attach, "\x1b[?1006l\x1b[?1000l", 2*time.Second) {
		t.Fatalf("exiting attach scrollback did not disable mouse reporting")
	}
}

func TestMultiAttachReconnectDisablesScrollbackMouseReporting(t *testing.T) {
	h, sessionID := startMouseReportingHost(t)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	ptytest.Advance(h.Clock(), 200*time.Millisecond)
	_ = attach.DrainRaw()

	attach.SendBytes([]byte{0x0c, '['})
	if !waitForRawContains(t, attach, "\x1b[?1000h\x1b[?1006h", 2*time.Second) {
		t.Fatalf("entering multi attach scrollback did not enable mouse reporting")
	}

	h.StopServer()
	h.RestartServer()
	waitForHost(t, h, sessionID, 15*time.Second)
	waitForClientCount(t, h, sessionID, 1, 15*time.Second)
	if !waitForRawContains(t, attach, "\x1b[?1006l\x1b[?1000l", 2*time.Second) {
		t.Fatalf("replacing active multi attach view did not disable mouse reporting")
	}
}

func startMouseReportingHost(t *testing.T) (*ptytest.Harness, string) {
	t.Helper()
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	sessionID := "mouse-attach"
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        20,
	})
	t.Cleanup(host.Cancel)
	waitForHost(t, h, sessionID, 3*time.Second)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	return h, sessionID
}
