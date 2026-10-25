package attach_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestMultiAttachCountdownOnServerDisconnect(t *testing.T) {
	h := newHarness(t)
	started := h.Clock().Now()
	t.Cleanup(func() {
		if got := h.Clock().Now().Sub(started); got != 1100*time.Millisecond {
			t.Errorf("readiness and frame observations advanced application time: got %v, want only the explicit 1.1s countdown step", got)
		}
	})

	// The owned process acknowledges input with output, independently of shell
	// startup files, prompts and terminal input echo.
	shell := filepath.Join(t.TempDir(), "countdown-shell.sh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nset -eu\nstty -echo\nprintf '\\nCOUNTDOWN_READY\\n'\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	published := make(chan struct{})
	var publishOnce sync.Once
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_disconnect_countdown",
		SessionName: "session_disconnect_countdown",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
		OnPublishStatus: func(status session.PublishStatus) {
			if status.Kind == session.PublishStatusConnected {
				publishOnce.Do(func() { close(published) })
			}
		},
	})
	t.Cleanup(host.Cancel)
	awaitTabSwitchEvent(t, published)
	waitTabSwitchFrame(t, host, "COUNTDOWN_READY", "")

	ready := make(chan struct{})
	var readyOnce sync.Once
	attached := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "session_disconnect_countdown",
		Cols:      80,
		Rows:      24,
		// Keep the first retry pending while observing a full countdown tick.
		BackoffPolicy: backoff.Policy{Base: 4 * time.Second, Max: 4 * time.Second},
		OnView: func(_ string, client *attach.Client) {
			onReady := client.OnReady
			client.OnReady = func() {
				onReady()
				readyOnce.Do(func() { close(ready) })
			}
		},
	})
	t.Cleanup(attached.Cancel)
	awaitTabSwitchEvent(t, ready)
	waitTabSwitchFrame(t, attached, "COUNTDOWN_READY", "")
	// An actual input round trip proves the attach input loop and its periodic
	// overlay ticker are installed before the clock is advanced.
	attached.Send("ready-countdown\n")
	waitTabSwitchFrame(t, host, "ready-countdown", "")
	waitTabSwitchFrame(t, attached, "ready-countdown", "")

	waitCountdown := func(detail string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		before := h.Clock().Now()
		if err := attached.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
			return screen.Contains("Not connected") && screen.Contains(detail)
		}); err != nil {
			t.Fatalf("waiting for disconnect countdown %q: %v; screen:\n%s", detail, err, attached.Screen().String())
		}
		if !h.Clock().Now().Equal(before) {
			t.Fatal("countdown observation advanced application time")
		}
	}

	h.StopServer()
	waitCountdown("reconnecting in 4s")
	h.Advance(1100 * time.Millisecond)
	waitCountdown("reconnecting in 3s")
}
