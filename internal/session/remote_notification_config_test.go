package session

import (
	"context"
	"io"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
)

type remoteRecordingNotifier struct{}

func (remoteRecordingNotifier) Notify(context.Context, desktopnotify.Request) error { return nil }

func TestRemoteManagerConnectViewPropagatesDesktopNotificationConfig(t *testing.T) {
	clk := clock.NewMock()
	notifier := remoteRecordingNotifier{}
	rm := newRemoteManager(remoteOptions{
		Endpoint:                    "https://relay.example/v1",
		Token:                       "token",
		LocalID:                     "local",
		LocalName:                   "local",
		Clock:                       clk,
		DisableDesktopNotifications: true,
		DesktopNotifier:             notifier,
	})
	rm.sessions = []remoteSessionInfo{{ID: "remote-1", Name: "remote-1"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := &remoteView{id: "remote-1"}
	if err := rm.connectView(ctx, view, io.Discard, nil); err != nil {
		t.Fatalf("connectView: %v", err)
	}
	if view.client == nil {
		t.Fatalf("expected connectView to create attach client")
	}
	if !view.client.DisableDesktopNotifications {
		t.Fatalf("expected remote attach client to inherit disabled desktop notifications")
	}
	if view.client.DesktopNotifier != notifier {
		t.Fatalf("expected remote attach client to preserve injected notifier, got %T", view.client.DesktopNotifier)
	}
	if view.cancel != nil {
		view.cancel()
	}
}
