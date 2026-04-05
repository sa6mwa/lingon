package attach

import (
	"context"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/protocolpb"
)

type recordingNotifier struct {
	requests []desktopnotify.Request
}

func (n *recordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.requests = append(n.requests, req)
	return nil
}

func TestClientNotifyDesktopForRemoteInactivityWall(t *testing.T) {
	notifier := &recordingNotifier{}
	client := &Client{
		Endpoint:        "https://relay.example/v1",
		DesktopNotifier: notifier,
		runCtx:          context.Background(),
	}

	client.notifyDesktop(&protocolpb.Wall{Message: "session-a inactive"})

	if len(notifier.requests) != 1 {
		t.Fatalf("expected one notification, got %d", len(notifier.requests))
	}
	if notifier.requests[0].Title != "session-a" {
		t.Fatalf("Title = %q, want session-a", notifier.requests[0].Title)
	}
	if notifier.requests[0].Body != "inactive" {
		t.Fatalf("Body = %q, want inactive", notifier.requests[0].Body)
	}
}

func TestClientNotifyDesktopSkipsLocalAndDisabled(t *testing.T) {
	notifier := &recordingNotifier{}
	client := &Client{
		Endpoint:                    "local://headless",
		DesktopNotifier:             notifier,
		DisableDesktopNotifications: true,
		runCtx:                      context.Background(),
	}

	client.notifyDesktop(&protocolpb.Wall{Message: "session-a inactive"})
	client.DisableDesktopNotifications = false
	client.Endpoint = "local://headless"
	client.notifyDesktop(&protocolpb.Wall{Message: "session-a inactive"})

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}

func TestClientNotifyDesktopSkipsUnixSocketAndNonInactivity(t *testing.T) {
	notifier := &recordingNotifier{}
	client := &Client{
		Endpoint:        "https://relay.example/v1",
		UnixSocket:      "/tmp/lingon.sock",
		DesktopNotifier: notifier,
		runCtx:          context.Background(),
	}

	client.notifyDesktop(&protocolpb.Wall{Message: "session-a inactive"})
	client.UnixSocket = ""
	client.notifyDesktop(&protocolpb.Wall{Message: "hello operators"})

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}
