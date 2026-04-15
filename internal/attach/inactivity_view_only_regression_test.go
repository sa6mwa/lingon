package attach_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/ptytest"
)

type attachRecordingNotifier struct {
	mu       sync.Mutex
	requests []desktopnotify.Request
}

func (n *attachRecordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.mu.Lock()
	n.requests = append(n.requests, req)
	n.mu.Unlock()
	return nil
}

func (n *attachRecordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.requests)
}

func TestAttachConnectDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &attachRecordingNotifier{}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:       "attach-wall-connect",
		SessionName:     "attach-wall-connect",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-connect"})
	host.Send("echo ATTACH_WALL_CONNECT_READY\n")
	if !screenContainsWithin(host, "ATTACH_WALL_CONNECT_READY", 2*time.Second) {
		t.Fatalf("expected source session marker before enabling inactivity")
	}

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	host.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		if notifier.count() != 1 {
			return fmt.Errorf("waiting for first inactivity notification")
		}
		return nil
	})

	attachSess := h.StartAttach(ptytest.AttachOptions{
		SessionID: "attach-wall-connect",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	if !screenContainsWithin(attachSess, "ATTACH_WALL_CONNECT_READY", 3*time.Second) {
		t.Fatalf("expected attach to render source session after connect")
	}

	time.Sleep(1500 * time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected attach connect/replay to avoid rearming inactivity, got %d notifications", got)
	}
}

func TestMultiAttachTabSwitchDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &attachRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "attach-wall-switch-a",
		SessionName:     "attach-wall-switch-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-wall-switch-b",
		SessionName: "attach-wall-switch-b",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-switch-a", "attach-wall-switch-b"})
	hostA.Send("echo ATTACH_WALL_SWITCH_A\n")
	hostB.Send("echo ATTACH_WALL_SWITCH_B\n")
	if !screenContainsWithin(hostA, "ATTACH_WALL_SWITCH_A", 2*time.Second) {
		t.Fatalf("expected source session marker before enabling inactivity")
	}
	if !screenContainsWithin(hostB, "ATTACH_WALL_SWITCH_B", 2*time.Second) {
		t.Fatalf("expected peer session marker before attach")
	}

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		if notifier.count() != 1 {
			return fmt.Errorf("waiting for first inactivity notification")
		}
		return nil
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-wall-switch-b",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	if !screenContainsWithin(attachSess, "ATTACH_WALL_SWITCH_B", 3*time.Second) {
		t.Fatalf("expected multi-attach to render initial session before tab switch")
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")
	if !screenContainsWithin(attachSess, "ATTACH_WALL_SWITCH_A", 3*time.Second) {
		t.Fatalf("expected multi-attach tab switch to render monitored session")
	}

	time.Sleep(1500 * time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected multi-attach tab switch to avoid rearming inactivity, got %d notifications", got)
	}
}
