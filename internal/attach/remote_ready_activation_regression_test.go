package attach_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func startTabSwitchHost(t *testing.T, h *ptytest.Harness, id, name, token string) *ptytest.PTYSession {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "tab-switch-shell.sh")
	script := fmt.Sprintf("#!/bin/sh\nset -eu\nstty -echo\ni=1\nwhile [ $i -le 20 ]; do printf '%%s_%%s\\n' '%s' \"$i\"; i=$((i+1)); done\nwhile IFS= read -r line; do :; done\n", token)
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	connected := make(chan struct{})
	var once sync.Once
	host := h.StartHost(ptytest.HostOptions{
		SessionID: id, SessionName: name, Shell: shell, Cols: 100, Rows: 30,
		OnPublishStatus: func(status session.PublishStatus) {
			if status.Kind == session.PublishStatusConnected {
				once.Do(func() { close(connected) })
			}
		},
	})
	t.Cleanup(host.Cancel)
	awaitTabSwitchEvent(t, connected)
	waitTabSwitchFrame(t, host, token+"_20", "")
	return host
}

func awaitTabSwitchEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-event:
	case <-ctx.Done():
		t.Fatal("waiting for tab-switch event:", ctx.Err())
	}
}

func waitTabSwitchFrame(t *testing.T, sess *ptytest.PTYSession, want, absent string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	before := sess.Clock().Now()
	if err := sess.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
		return screen.Contains(want) && (absent == "" || !screen.Contains(absent))
	}); err != nil {
		t.Fatalf("waiting for %q without %q: %v; screen:\n%s", want, absent, err, sess.Screen().String())
	}
	if !sess.Clock().Now().Equal(before) {
		t.Fatal("frame observation advanced application time")
	}
}

func TestAttachLateReadyDoesNotAnnounceInactiveView(t *testing.T) {
	h := newHarness(t)
	startTabSwitchHost(t, h, "late-ready-a", "a", "FRAME_A")
	startTabSwitchHost(t, h, "late-ready-b", "b", "FRAME_B")
	ctx, cancel := context.WithCancel(t.Context())
	clk := &readinessRegistrationClock{MockClock: clock.NewMock(), ctx: ctx, reached: make(chan struct{}), release: make(chan struct{})}
	firstReady, secondReady := make(chan struct{}), make(chan struct{})
	active := make(chan string, 16)
	var eventsMu sync.Mutex
	var events []string
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "late-ready-a", Cols: 100, Rows: 30, Clock: clk,
		OnView: func(id string, client *attach.Client) {
			ready := client.OnReady
			client.OnReady = func() {
				if id == "late-ready-b" {
					clk.armed.Store(true)
					ready()
					close(secondReady)
				} else {
					ready()
					close(firstReady)
				}
			}
		},
		OnActive: func(id string) {
			eventsMu.Lock()
			events = append(events, id)
			eventsMu.Unlock()
			active <- id
		},
	})
	t.Cleanup(sess.Cancel)
	t.Cleanup(cancel)
	awaitActive := func(want string) {
		t.Helper()
		ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
		defer stop()
		select {
		case got := <-active:
			if got != want {
				t.Fatalf("active callback = %q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal("waiting for active callback:", ctx.Err())
		}
	}
	awaitTabSwitchEvent(t, firstReady)
	awaitActive("late-ready-a")
	waitTabSwitchFrame(t, sess, "FRAME_A_20", "FRAME_B")
	sess.Send("\x0cn")
	awaitActive("late-ready-b")
	awaitTabSwitchEvent(t, clk.reached)
	waitTabSwitchFrame(t, sess, "FRAME_B_20", "FRAME_A")
	sess.Send("\x0cn")
	awaitActive("late-ready-a")
	waitTabSwitchFrame(t, sess, "FRAME_A_20", "FRAME_B")
	close(clk.release)
	awaitTabSwitchEvent(t, secondReady)
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 3 || events[2] != "late-ready-a" {
		t.Fatalf("late readiness announced an inactive view: %v", events)
	}
	if screen := sess.Screen(); !screen.Contains("FRAME_A_20") || screen.Contains("FRAME_B") {
		t.Fatalf("late readiness replaced the active frame:\n%s", screen.String())
	}
}
