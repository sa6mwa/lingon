package headlessd

import (
	"context"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
)

type recordingNotifier struct {
	requests []desktopnotify.Request
}

func (n *recordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.requests = append(n.requests, req)
	return nil
}

func TestDaemonNotifyDesktopForInactivity(t *testing.T) {
	notifier := &recordingNotifier{}
	d := New(Options{
		SessionID:       "session-a",
		DesktopNotifier: notifier,
	})
	d.sessionID = "session-a"

	d.notifyDesktop("session-a inactive")

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

	d.notifyDesktop("session-a inactive")

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

	d.notifyDesktop("hello operators")

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}
