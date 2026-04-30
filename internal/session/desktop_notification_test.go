package session

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
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

func TestRunnerShowWallPreservesScrollbackViewport(t *testing.T) {
	const cols, rows = 80, 8
	clk := clock.NewMock()
	live := makeSnapshot(cols, rows, 0, rows-1, 0, -1, -1)
	setRow(live, 0, "LIVE-END-TOKEN")
	local := &localSession{
		id:       "s1",
		name:     "session-a",
		clock:    clk,
		snapshot: live,
		scroll: []terminal.ScrollbackRow{
			scrollbackTestRow(cols, "HISTORY-TOKEN-00"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-01"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-02"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-03"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-04"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-05"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-06"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-07"),
		},
	}
	r := &Runner{
		opts: Options{
			Cols: cols,
			Rows: rows,
		},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": local,
		},
	}
	r.setActiveSession("s1", true)
	r.scrollbackMu.Lock()
	r.scrollbackSession = "s1"
	r.scrollbackView.EnterAt(len(local.scroll)+int(live.Rows), rows, len(local.scroll), cols, cols, 0)
	r.scrollbackMu.Unlock()

	stdout, readOutput := openTestTTY(t, cols, rows)
	defer func() {
		_ = stdout.Close()
	}()

	r.renderScrollback(stdout, nil)
	if out := readOutput(); !strings.Contains(out, "HISTORY-TOKEN") {
		t.Fatalf("expected initial scrollback render to show history, got %q", out)
	}

	r.showWall(&protocolpb.Wall{
		Sender:         "alice@relay",
		Message:        "hello from wall",
		TimeoutSeconds: 5,
	}, stdout)
	out := readOutput()
	if strings.Contains(out, "LIVE-END-TOKEN") {
		t.Fatalf("wall modal repaint rendered live viewport instead of preserving scrollback viewport: %q", out)
	}
	if !strings.Contains(out, "hello from wall") {
		t.Fatalf("expected wall modal bytes, got %q", out)
	}
}

func TestRunnerShowWallPreservesMixedScrollbackAndLiveViewport(t *testing.T) {
	const cols, rows = 80, 8
	clk := clock.NewMock()
	live := makeSnapshot(cols, rows, 0, rows-1, 0, -1, -1)
	setRow(live, 0, "LIVE-VISIBLE-TOKEN")
	setRow(live, rows-1, "LIVE-END-TOKEN")
	local := &localSession{
		id:       "s1",
		name:     "session-a",
		clock:    clk,
		snapshot: live,
		scroll: []terminal.ScrollbackRow{
			scrollbackTestRow(cols, "HISTORY-TOKEN-00"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-01"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-02"),
			scrollbackTestRow(cols, "HISTORY-TOKEN-03"),
		},
	}
	r := &Runner{
		opts: Options{
			Cols: cols,
			Rows: rows,
		},
		runCtx: context.Background(),
		clock:  clk,
		localSessions: map[string]*localSession{
			"s1": local,
		},
	}
	r.setActiveSession("s1", true)
	r.scrollbackMu.Lock()
	r.scrollbackSession = "s1"
	r.scrollbackView.EnterAt(len(local.scroll)+int(live.Rows), rows, len(local.scroll), cols, cols, 0)
	r.scrollbackMu.Unlock()

	stdout, readOutput := openTestTTY(t, cols, rows)
	defer func() {
		_ = stdout.Close()
	}()

	r.renderScrollback(stdout, nil)
	initial := readOutput()
	if !strings.Contains(initial, "HISTORY-TOKEN") {
		t.Fatalf("expected mixed viewport to include history, got %q", initial)
	}
	if !strings.Contains(initial, "LIVE-VISIBLE-TOKEN") {
		t.Fatalf("expected mixed viewport to include visible live rows, got %q", initial)
	}

	r.showWall(&protocolpb.Wall{
		Sender:         "alice@relay",
		Message:        "hello from mixed wall",
		TimeoutSeconds: 5,
	}, stdout)
	out := readOutput()
	if strings.Contains(out, "LIVE-END-TOKEN") {
		t.Fatalf("wall modal repaint rendered outside the mixed viewport into live end content: %q", out)
	}
	if !strings.Contains(out, "hello from mixed wall") {
		t.Fatalf("expected wall modal bytes, got %q", out)
	}
}

func openTestTTY(t *testing.T, cols, rows int) (*os.File, func() string) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatalf("pty.Setsize: %v", err)
	}
	var buf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, master)
		close(readDone)
	}()
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
		<-readDone
	})
	return slave, func() string {
		t.Helper()
		time.Sleep(10 * time.Millisecond)
		out := buf.String()
		buf.Reset()
		return out
	}
}

func scrollbackTestRow(cols int, content string) terminal.ScrollbackRow {
	cells := make([]terminal.Cell, cols)
	for i := 0; i < cols; i++ {
		r := ' '
		if i < len(content) {
			r = rune(content[i])
		}
		cells[i] = terminal.Cell{Rune: r}
	}
	return terminal.ScrollbackRow{Cols: cols, Cells: cells}
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
