package attach_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRelayWallInactivityStatusFansOutToPeer(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-relay-wall-status",
		SessionName: "attach-relay-wall-status",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-relay-wall-status"})

	host.Send("echo ATTACH_RELAY_WALL_STATUS_READY\n")
	if !screenContainsWithin(host, "ATTACH_RELAY_WALL_STATUS_READY", 2*time.Second) {
		t.Fatalf("expected source session marker before attach clients connect")
	}

	attachA := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "attach-relay-wall-status",
		ClientID:       "attach-wall-a",
		RequestControl: true,
		Cols:           120,
		Rows:           30,
	})
	t.Cleanup(attachA.Cancel)
	attachB := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "attach-relay-wall-status",
		ClientID:       "attach-wall-b",
		RequestControl: true,
		Cols:           120,
		Rows:           30,
	})
	t.Cleanup(attachB.Cancel)

	if !screenContainsWithin(attachA, "ATTACH_RELAY_WALL_STATUS_READY", 3*time.Second) {
		t.Fatalf("expected first attach client to render source session")
	}
	if !screenContainsWithin(attachB, "ATTACH_RELAY_WALL_STATUS_READY", 3*time.Second) {
		t.Fatalf("expected second attach client to render source session")
	}

	attachA.SendCtrlL()
	attachA.Send("w")

	attachA.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "wall inactivity 2m") {
			return fmt.Errorf("expected source attach status fanout, row=%q", row)
		}
		return nil
	})
	attachB.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "wall inactivity 2m") {
			return fmt.Errorf("expected peer attach status fanout, row=%q", row)
		}
		return nil
	})
}

func TestMultiAttachWallStatusBannerDoesNotBlockPromptRepaint(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-relay-wall-banner-repaint",
		SessionName: "attach-relay-wall-banner-repaint",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-relay-wall-banner-repaint"})
	host.Send("echo BANNER_REPAINT_READY\n")
	if !screenContainsWithin(host, "BANNER_REPAINT_READY", 2*time.Second) {
		t.Fatalf("expected host readiness marker before attach")
	}

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-relay-wall-banner-repaint",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attach.Cancel)

	if !screenContainsWithin(attach, "BANNER_REPAINT_READY", 3*time.Second) {
		t.Fatalf("expected attach to render readiness marker before banner test")
	}

	attach.SendCtrlL()
	attach.Send("w")
	if !screenContainsWithinRealTime(attach, "wall inactivity 2m", 2*time.Second) {
		t.Fatalf("expected wall inactivity banner on attach, got:\n%s", attach.Screen().String())
	}

	attach.Send("echo BANNER_REPAINT_OK\n")
	if !screenContainsWithin(host, "BANNER_REPAINT_OK", 350*time.Millisecond) {
		t.Fatalf("expected host marker to execute promptly, got:\n%s", host.Screen().String())
	}
	attach.Eventually(350*time.Millisecond, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), "wall inactivity 2m") {
			return fmt.Errorf("expected wall inactivity banner on row 0, row=%q", screen.Row(0))
		}
		if !strings.Contains(screen.String(), "BANNER_REPAINT_OK") {
			return fmt.Errorf("expected attach output marker while banner visible, got:\n%s", screen.String())
		}
		return nil
	})
}

func TestMultiAttachSwitchToDisconnectedRelayTabDoesNotShowWallInactivityOff(t *testing.T) {
	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-wall-status-switch-a",
		SessionName: "attach-wall-status-switch-a",
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-wall-status-switch-b",
		SessionName: "attach-wall-status-switch-b",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-status-switch-a", "attach-wall-status-switch-b"})

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-wall-status-switch-a",
		Cols:      100,
		Rows:      30,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	waitForClientReady(t, h.Clock(), &viewsMu, views, "attach-wall-status-switch-a", 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitForClientReady(t, h.Clock(), &viewsMu, views, "attach-wall-status-switch-b", 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("p")
	waitForClientReady(t, h.Clock(), &viewsMu, views, "attach-wall-status-switch-a", 3*time.Second)

	hostB.Cancel()
	ptytest.Advance(h.Clock(), 1*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Eventually(3*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.Row(0), "wall inactivity off") {
			return fmt.Errorf("unexpected wall inactivity banner while switching to disconnected relay tab: %q", screen.Row(0))
		}
		return nil
	})
}
