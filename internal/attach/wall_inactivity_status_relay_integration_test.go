package attach_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
