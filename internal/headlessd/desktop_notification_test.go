package headlessd

import (
	"context"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/protocolpb"
)

type recordingNotifier struct {
	mu       sync.Mutex
	requests []desktopnotify.Request
}

func (n *recordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.mu.Lock()
	n.requests = append(n.requests, req)
	n.mu.Unlock()
	return nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.requests)
}

func TestDaemonNotifyDesktopForInactivity(t *testing.T) {
	notifier := &recordingNotifier{}
	d := New(Options{
		SessionID:       "session-a",
		DesktopNotifier: notifier,
	})
	d.sessionID = "session-a"

	d.notifyDesktop(&protocolpb.Wall{
		Message:         "session-a inactive",
		Kind:            protocolpb.WallKind_WALL_KIND_INACTIVITY,
		SourceSessionId: "session-a",
	})

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

func TestDaemonNotifyDesktopSkipsWhenDisabled(t *testing.T) {
	notifier := &recordingNotifier{}
	d := New(Options{
		SessionID:                   "session-a",
		DesktopNotifier:             notifier,
		DisableDesktopNotifications: true,
	})

	d.notifyDesktop(&protocolpb.Wall{
		Message:         "session-a inactive",
		Kind:            protocolpb.WallKind_WALL_KIND_INACTIVITY,
		SourceSessionId: "session-a",
	})

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}

func TestDaemonNotifyDesktopSkipsNonInactivityWall(t *testing.T) {
	notifier := &recordingNotifier{}
	d := New(Options{
		SessionID:       "session-a",
		DesktopNotifier: notifier,
	})

	d.notifyDesktop(&protocolpb.Wall{Message: "hello operators"})

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}

func TestDaemonNotifyDesktopUsesNotifierFactoryWhenUnset(t *testing.T) {
	restore := desktopnotify.SetFactoryForTesting(func() desktopnotify.Notifier {
		return &recordingNotifier{}
	})
	defer restore()

	d := New(Options{
		SessionID: "session-a",
	})

	d.notifyDesktop(&protocolpb.Wall{
		Message:         "session-a inactive",
		Kind:            protocolpb.WallKind_WALL_KIND_INACTIVITY,
		SourceSessionId: "session-a",
	})

	notifier, ok := d.desktopNotifier.(*recordingNotifier)
	if !ok {
		t.Fatalf("expected factory notifier, got %T", d.desktopNotifier)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected one notification via factory notifier, got %d", notifier.count())
	}
}
