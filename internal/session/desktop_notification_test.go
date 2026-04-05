package session

import (
	"context"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
)

type recordingNotifier struct {
	requests []desktopnotify.Request
}

func (n *recordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.requests = append(n.requests, req)
	return nil
}

func TestRunnerLocalWallNotificationFiresOnceUntilActivityResets(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := clock.NewMock()
	r := &Runner{
		opts:   Options{DesktopNotifier: notifier},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk},
		},
	}

	r.configureLocalWallNotification("s1", 2*time.Minute)
	clk.Add(2 * time.Minute)

	if len(notifier.requests) != 1 {
		t.Fatalf("expected one notification, got %d", len(notifier.requests))
	}
	if notifier.requests[0].Title != "session-a" {
		t.Fatalf("Title = %q, want session-a", notifier.requests[0].Title)
	}
	if notifier.requests[0].Body != "inactive" {
		t.Fatalf("Body = %q, want inactive", notifier.requests[0].Body)
	}

	clk.Add(2 * time.Minute)
	if len(notifier.requests) != 1 {
		t.Fatalf("expected one notification without rearming, got %d", len(notifier.requests))
	}

	r.noteLocalActivity("s1")
	clk.Add(2 * time.Minute)
	if len(notifier.requests) != 2 {
		t.Fatalf("expected notification after activity reset, got %d", len(notifier.requests))
	}
}

func TestRunnerLocalWallNotificationSkipsWhenDisabled(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := clock.NewMock()
	r := &Runner{
		opts: Options{
			DesktopNotifier:             notifier,
			DisableDesktopNotifications: true,
		},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk},
		},
	}

	r.configureLocalWallNotification("s1", time.Minute)
	clk.Add(time.Minute)

	if len(notifier.requests) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notifier.requests))
	}
}

func TestRunnerLocalWallNotificationIsPerSession(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := clock.NewMock()
	r := &Runner{
		opts:   Options{DesktopNotifier: notifier},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk},
			"s2": {id: "s2", name: "session-b", clock: clk},
		},
	}

	r.configureLocalWallNotification("s1", 2*time.Minute)
	r.configureLocalWallNotification("s2", time.Minute)
	r.noteLocalActivity("s1")

	clk.Add(time.Minute)
	if len(notifier.requests) != 1 {
		t.Fatalf("expected only s2 notification after one minute, got %d", len(notifier.requests))
	}
	if notifier.requests[0].Title != "session-b" {
		t.Fatalf("first Title = %q, want session-b", notifier.requests[0].Title)
	}

	clk.Add(time.Minute)
	if len(notifier.requests) != 2 {
		t.Fatalf("expected second notification for rearmed s1, got %d", len(notifier.requests))
	}
	if notifier.requests[1].Title != "session-a" {
		t.Fatalf("second Title = %q, want session-a", notifier.requests[1].Title)
	}
}

func TestRunnerDisableLocalWallNotificationCancelsPendingTimer(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := clock.NewMock()
	r := &Runner{
		opts:   Options{DesktopNotifier: notifier},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk},
		},
	}

	r.configureLocalWallNotification("s1", time.Minute)
	r.disableLocalWallNotification("s1")
	clk.Add(2 * time.Minute)

	if len(notifier.requests) != 0 {
		t.Fatalf("expected disabled notification timer to stay silent, got %d", len(notifier.requests))
	}
}
