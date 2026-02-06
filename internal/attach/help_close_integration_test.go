package attach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachHelpModalClosesOnQAndQOnly(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-help-close",
		SessionName: "host-help-close",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-help-close"})

	var viewMu sync.Mutex
	var viewClient *attach.Client
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-help-close",
		Cols:      80,
		Rows:      24,
		OnView: func(sessionID string, client *attach.Client) {
			if sessionID != "host-help-close" {
				return
			}
			viewMu.Lock()
			viewClient = client
			viewMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	helpVisible := func() bool {
		viewMu.Lock()
		client := viewClient
		viewMu.Unlock()
		if client == nil || client.Compositor() == nil {
			return false
		}
		return client.Compositor().HelpVisible()
	}
	waitForView := func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			viewMu.Lock()
			ready := viewClient != nil
			viewMu.Unlock()
			if ready {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("view client not ready before timeout")
	}
	waitFor := func(cond func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if cond() {
			return
		}
		screen := attachSess.Screen().String()
		t.Fatalf("condition not met before timeout:\n%s", screen)
	}
	showHelp := func() {
		attachSess.SendBytes([]byte{0x0c, 'h'})
		waitFor(helpVisible)
	}
	waitForHelpHidden := func() {
		waitFor(func() bool { return !helpVisible() })
	}

	waitForView()
	showHelp()
	attachSess.SendBytes([]byte{'q'})
	waitForHelpHidden()

	showHelp()
	attachSess.SendBytes([]byte{'Q'})
	waitForHelpHidden()

	showHelp()
	attachSess.SendBytes([]byte{0x1b})
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !helpVisible() {
			t.Fatalf("expected help to remain visible after ESC")
		}
		time.Sleep(20 * time.Millisecond)
	}

	attachSess.Send("\n")
	deadline = time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !helpVisible() {
			t.Fatalf("expected help to remain visible after Enter")
		}
		time.Sleep(20 * time.Millisecond)
	}
	attachSess.SendBytes([]byte{'q'})
	waitForHelpHidden()
}

func TestAttachHelpModalBlocksInputUntilDismissed(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-help-input-gate-attach",
		SessionName: "host-help-input-gate-attach",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-help-input-gate-attach"})

	var viewMu sync.Mutex
	var viewClient *attach.Client
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-help-input-gate-attach",
		Cols:      80,
		Rows:      24,
		OnView: func(sessionID string, client *attach.Client) {
			if sessionID != "host-help-input-gate-attach" {
				return
			}
			viewMu.Lock()
			viewClient = client
			viewMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	helpVisible := func() bool {
		viewMu.Lock()
		client := viewClient
		viewMu.Unlock()
		if client == nil || client.Compositor() == nil {
			return false
		}
		return client.Compositor().HelpVisible()
	}
	waitFor := func(cond func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if cond() {
			return
		}
		screen := attachSess.Screen().String()
		t.Fatalf("condition not met before timeout:\n%s", screen)
	}
	waitFor(func() bool {
		viewMu.Lock()
		ready := viewClient != nil
		viewMu.Unlock()
		return ready
	})

	attachSess.SendBytes([]byte{0x0c, 'h'})
	waitFor(helpVisible)

	const blockedToken = "ATTACH_BLOCKED_BY_HELP"
	attachSess.Send("echo " + blockedToken + "\n")
	if screenContainsWithin(attachSess, blockedToken, 700*time.Millisecond) {
		t.Fatalf("expected attach input blocked while help modal visible")
	}

	attachSess.SendBytes([]byte{'q'})
	waitFor(func() bool { return !helpVisible() })

	if screenContainsWithin(attachSess, blockedToken, 700*time.Millisecond) {
		t.Fatalf("expected blocked attach input discarded after help close")
	}

	const afterToken = "ATTACH_AFTER_HELP"
	attachSess.Send("echo " + afterToken + "\n")
	if !screenContainsWithin(attachSess, afterToken, 2*time.Second) {
		t.Fatalf("expected attach input restored after help close")
	}
}
