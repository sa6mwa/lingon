package session

import (
	"context"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
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

func TestRunnerLocalWallNotificationUsesNotifierFactoryWhenUnset(t *testing.T) {
	notifier := &recordingNotifier{}
	restore := desktopnotify.SetFactoryForTesting(func() desktopnotify.Notifier { return notifier })
	defer restore()

	clk := clock.NewMock()
	r := &Runner{
		opts:   Options{},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk},
		},
	}

	r.configureLocalWallNotification("s1", time.Minute, "1m", false)
	clk.Add(time.Minute)

	if len(notifier.requests) != 1 {
		t.Fatalf("expected one notification from factory-backed notifier, got %d", len(notifier.requests))
	}
	if notifier.requests[0].Title != "session-a" || notifier.requests[0].Body != "inactive" {
		t.Fatalf("unexpected notification %+v", notifier.requests[0])
	}
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

	r.configureLocalWallNotification("s1", 2*time.Minute, "2m", false)
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

	r.configureLocalWallNotification("s1", time.Minute, "1m", false)
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

	r.configureLocalWallNotification("s1", 2*time.Minute, "2m", false)
	r.configureLocalWallNotification("s2", time.Minute, "1m", false)
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

	r.configureLocalWallNotification("s1", time.Minute, "1m", false)
	r.disableLocalWallNotification("s1")
	clk.Add(2 * time.Minute)

	if len(notifier.requests) != 0 {
		t.Fatalf("expected disabled notification timer to stay silent, got %d", len(notifier.requests))
	}
}

func TestRunnerToggleWallInactivityFallbackArmsLocalWallNotification(t *testing.T) {
	notifier := &recordingNotifier{}
	clk := clock.NewMock()
	stdout, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer stdout.Close()

	r := &Runner{
		opts: Options{
			DesktopNotifier: notifier,
			ToggleWallInactivityFallback: func(context.Context, string) (WallInactivityToggleResult, error) {
				return WallInactivityToggleResult{
					Enabled:       true,
					InactiveAfter: "2m",
				}, nil
			},
		},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": {id: "s1", name: "session-a", clock: clk, offline: true},
		},
	}

	r.toggleWallInactivity(context.Background(), "s1", nil, stdout)

	r.wallNotifyMu.Lock()
	after := r.wallNotifyAfter["s1"]
	armed := r.wallNotifyArmed["s1"]
	r.wallNotifyMu.Unlock()
	if after != 2*time.Minute {
		t.Fatalf("wallNotifyAfter = %v, want 2m", after)
	}
	if !armed {
		t.Fatalf("expected local inactivity timer to be armed")
	}

	clk.Add(2 * time.Minute)
	if len(notifier.requests) != 1 {
		t.Fatalf("expected one notification after fallback arm, got %d", len(notifier.requests))
	}
	if notifier.requests[0].Title != "session-a" {
		t.Fatalf("Title = %q, want session-a", notifier.requests[0].Title)
	}
}

func TestRunnerShowWallKeepsModalVisibleWhenDesktopNotifierConfigured(t *testing.T) {
	notifier := &recordingNotifier{}
	r := &Runner{
		opts:   Options{DesktopNotifier: notifier},
		runCtx: context.Background(),
	}

	r.showWall(&protocolpb.Wall{
		Sender:            "alice@relay",
		Message:           "session-a inactive",
		TimeoutSeconds:    5,
		SourceSessionName: "build-host",
	}, nil)

	state := r.runtime().State()
	if !state.WallVisible {
		t.Fatalf("expected in-app wall modal to remain visible")
	}
	if state.WallTitle != "Broadcast from alice@relay#build-host:" {
		t.Fatalf("WallTitle = %q, want %q", state.WallTitle, "Broadcast from alice@relay#build-host:")
	}
	if state.WallMessage != "session-a inactive" {
		t.Fatalf("WallMessage = %q, want %q", state.WallMessage, "session-a inactive")
	}
	if len(notifier.requests) != 0 {
		t.Fatalf("expected wall rendering path not to emit desktop notifications directly, got %d", len(notifier.requests))
	}
}

func TestRunnerRelayBackedLocalWallNotificationSuppressesDuplicateRelayWall(t *testing.T) {
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
		activeSessionID: "s2",
		activeIsLocal:   true,
	}

	r.configureLocalWallNotification("s1", time.Minute, "1m", true)
	clk.Add(time.Minute)

	if len(notifier.requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(notifier.requests))
	}
	state := r.runtime().State()
	if !state.WallVisible {
		t.Fatalf("expected relay-backed local inactivity to keep same-host sibling modal behavior")
	}
	if state.WallMessage != "session-a inactive" {
		t.Fatalf("WallMessage = %q, want %q", state.WallMessage, "session-a inactive")
	}
	if !r.suppressRelayBackedLocalInactivityDuplicate(&protocolpb.Wall{
		Message:         "session-a inactive",
		Kind:            protocolpb.WallKind_WALL_KIND_INACTIVITY,
		SourceSessionId: "s1",
	}) {
		t.Fatalf("expected duplicate relay inactivity wall to be suppressed after local modal path")
	}
	if r.suppressRelayBackedLocalInactivityDuplicate(&protocolpb.Wall{
		Message:         "session-a inactive",
		Kind:            protocolpb.WallKind_WALL_KIND_INACTIVITY,
		SourceSessionId: "s1",
	}) {
		t.Fatalf("expected suppression token to be one-shot")
	}
}
