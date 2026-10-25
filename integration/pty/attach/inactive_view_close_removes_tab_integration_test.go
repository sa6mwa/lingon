//go:build integration
// +build integration

package integrationptyattach_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/testclock"
)

func TestAttachRemovesTabAfterInactiveViewTimeoutAndHostExit(t *testing.T) {
	restoreTabDelay := mvu.SetTabBarAutoHideDelay(24 * time.Hour)
	defer restoreTabDelay()

	clk := testclock.New()
	h := newHarness(t, ptytest.WithClock(clk))
	startedAt := clk.Now()
	defer func() {
		if !clk.Now().Equal(startedAt.Add(10 * time.Second)) {
			t.Error("UI observations advanced time beyond the explicit expiry steps")
		}
	}()
	awaitID := func(events <-chan string, want string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		for {
			select {
			case id := <-events:
				if (want == "" && id != "host-1") || id == want {
					return id
				}
			case <-ctx.Done():
				t.Fatalf("waiting for session %q event: %v", want, ctx.Err())
			}
		}
	}
	waitScreen := func(sess *ptytest.PTYSession, check func(ptytest.Screen) bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		before := clk.Now()
		if err := sess.WaitForScreen(ctx, check); err != nil {
			t.Fatalf("screen event: %v; screen:\n%s", err, sess.Screen().String())
		}
		if !clk.Now().Equal(before) {
			t.Fatal("screen observation advanced application time")
		}
	}
	published := make(chan string, 8)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1", Shell: fixedPromptEmitRowsBash(t), Cols: 120, Rows: 30,
		OnPublishStatus: func(status session.PublishStatus) {
			if status.Kind == session.PublishStatusConnected {
				published <- status.SessionID
			}
		},
	})
	t.Cleanup(host.Cancel)
	awaitID(published, "host-1")
	waitScreen(host, func(screen ptytest.Screen) bool { return screen.Contains("PROMPT>") })
	host.Send("\x0cc")
	secondID := awaitID(published, "")
	labelMap := sessionLabelMap(t, h.Endpoint(), h.AccessToken())
	primaryLabel, secondLabel := labelMap["host-1"], labelMap[secondID]
	if primaryLabel == "" || secondLabel == "" {
		t.Fatalf("missing session labels: %v", labelMap)
	}

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	ready := make(chan string, 8)
	closed := make(chan string, 8)
	removed := make(chan string, 8)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-1", Cols: 120, Rows: 30,
		InactiveTTL: time.Hour, RefreshInterval: 150 * time.Millisecond,
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
			onReady := client.OnReady
			client.OnReady = func() { onReady(); ready <- id }
			onSessions := client.OnSessions
			client.OnSessions = func(sessions []attach.SessionInfo) {
				onSessions(sessions)
				if id == "host-1" && len(sessions) == 1 && sessions[0].ID == "host-1" {
					select {
					case removed <- secondID:
					default:
					}
				}
			}
		},
		OnViewClosed: func(id string, visible, _ bool) {
			if id == secondID && !visible {
				closed <- id
			}
		},
	})
	t.Cleanup(attachSess.Cancel)

	awaitID(ready, "host-1")
	attachSess.Send("printf 'PRIMARY_%s\\n' READY\n")
	waitScreen(attachSess, func(screen ptytest.Screen) bool { return screen.Contains("PRIMARY_READY") })
	attachSess.Send("\x0cn")
	awaitID(ready, secondID)
	attachSess.Send("printf 'SECOND_%s\\n' READY\n")
	waitScreen(attachSess, func(screen ptytest.Screen) bool { return screen.Contains("SECOND_READY") })
	waitScreen(host, func(screen ptytest.Screen) bool { return screen.Contains("SECOND_READY") })
	attachSess.Send("\x0cp")
	waitScreen(attachSess, func(screen ptytest.Screen) bool {
		return screen.Contains("PRIMARY_READY") && !screen.Contains("SECOND_READY")
	})

	viewsMu.Lock()
	inactiveClient := views[secondID]
	viewsMu.Unlock()
	if inactiveClient == nil {
		t.Fatalf("missing inactive client for %q", secondID)
	}
	inactiveClient.Close("timeout")
	awaitID(closed, secondID)
	if inactiveClient.Connected() {
		t.Fatal("timed-out inactive view remains connected")
	}

	// Expire the connection banner explicitly, after both readiness callbacks
	// have finished. Observation must not install a fresh banner or move time.
	clk.Add(4 * time.Second)
	waitScreen(attachSess, func(screen ptytest.Screen) bool {
		row := screen.Row(0)
		return !strings.Contains(row, "connected to") && strings.Contains(row, primaryLabel) && strings.Contains(row, secondLabel)
	})

	host.SendBytes([]byte{0x04})
	awaitID(removed, secondID)
	// Let the established missing-session grace expire, without polling time.
	clk.Add(6 * time.Second)
	waitScreen(attachSess, func(screen ptytest.Screen) bool {
		row := screen.Row(0)
		return strings.Contains(row, primaryLabel) && !strings.Contains(row, secondLabel)
	})
	attachSess.Send("\x0cnprintf 'AFTER_EXIT_%s\\n' READY\n")
	waitScreen(attachSess, func(screen ptytest.Screen) bool { return screen.Contains("AFTER_EXIT_READY") })
}
