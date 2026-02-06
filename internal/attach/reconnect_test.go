package attach_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachReconnectsAfterServerRestart(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_attach",
		SessionName: "session_attach",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_attach"})

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "session_attach",
		Cols:      80,
		Rows:      24,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(func() {
		attach.Cancel()
		if ok, err := attach.WaitErr(2 * time.Second); ok {
			if err != nil && err != context.Canceled && !strings.Contains(err.Error(), "no sessions available") {
				t.Fatalf("attach exit error: %v", err)
			}
		}
	})

	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("connected to ") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})

	h.StopServer()

	if ok, err := attach.WaitErr(500 * time.Millisecond); ok {
		t.Fatalf("attach exited after server stop: %v", err)
	}
	if ok, err := host.WaitErr(500 * time.Millisecond); ok {
		t.Fatalf("host exited after server stop: %v", err)
	}

	h.RestartServer()
	if ok, err := attach.WaitErr(2 * time.Second); ok {
		if err != nil && err != context.Canceled && !strings.Contains(err.Error(), "no sessions available") {
			t.Fatalf("attach exit error: %v", err)
		}
	} else {
		waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_attach"}, 15*time.Second)
		waitForClientReady(t, h.Clock(), &viewsMu, views, "session_attach", 10*time.Second)
	}

	host.Cancel()
	_, _ = host.WaitErr(2 * time.Second)
}
