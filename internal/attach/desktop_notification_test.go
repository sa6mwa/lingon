package attach

import (
	"context"
	"io"
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

func TestClientHandleWallKeepsModalVisibleWhileNotifyingDesktop(t *testing.T) {
	notifier := &recordingNotifier{}
	client := &Client{
		Endpoint:        "https://relay.example/v1",
		DesktopNotifier: notifier,
		Stdout:          io.Discard,
		runCtx:          context.Background(),
	}

	client.handleWall(&protocolpb.Wall{
		Sender:         "alice@relay",
		Message:        "session-a inactive",
		TimeoutSeconds: 5,
	})

	if len(notifier.requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(notifier.requests))
	}
	state := client.ensureCompositor().State()
	if !state.WallVisible {
		t.Fatalf("expected in-app wall modal to remain visible")
	}
	if state.WallTitle != "Broadcast from alice@relay:" {
		t.Fatalf("WallTitle = %q, want %q", state.WallTitle, "Broadcast from alice@relay:")
	}
	if state.WallMessage != "session-a inactive" {
		t.Fatalf("WallMessage = %q, want %q", state.WallMessage, "session-a inactive")
	}
}
