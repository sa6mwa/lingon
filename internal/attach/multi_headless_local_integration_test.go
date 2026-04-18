package attach_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/pslog"
)

func TestMultiAttachSwitchesAcrossLocalHeadlessSocketsPTY(t *testing.T) {
	cfgDir := t.TempDir()

	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "headless-a",
	})
	defer stopA()
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "headless-b",
	})
	defer stopB()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})

	var activeMu sync.Mutex
	activeID := ""
	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "headless-a",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
		OnActive: func(sessionID string) {
			activeMu.Lock()
			activeID = sessionID
			activeMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "headless-a") || !strings.Contains(row, "headless-b") {
			return fmt.Errorf("expected headless tab labels on row 1, got %q", row)
		}
		if strings.Contains(row, " local ") {
			return fmt.Errorf("unexpected generic local label in tab row: %q", row)
		}
		return nil
	})

	waitUntilLocal(t, 8*time.Second, func() bool {
		activeMu.Lock()
		defer activeMu.Unlock()
		return activeID == "headless-a"
	})
	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitUntilLocal(t, 8*time.Second, func() bool {
		activeMu.Lock()
		defer activeMu.Unlock()
		return activeID == "headless-b"
	})
}

func TestMultiAttachHeadlessDiscoversNewSessionQuicklyWithDefaultRefresh(t *testing.T) {
	cfgDir := shortConfigDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessionEvents, stopWatcher := startHeadlessStateWatcher(t, ctx, cfgDir)
	defer stopWatcher()

	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "discover-a",
		Respawn:   true,
	})
	defer stopA()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	var activeMu sync.Mutex
	activeID := ""
	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "discover-a",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		// No RefreshInterval override: discovery should be event-driven via SessionEvents.
		SessionEvents:  sessionEvents,
		SessionSource:  localHeadlessSessionSource(cfgDir),
		SocketResolver: localHeadlessSocketResolver(cfgDir),
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
	})
	defer attachSess.Cancel()

	waitUntilLocal(t, 8*time.Second, func() bool {
		activeMu.Lock()
		defer activeMu.Unlock()
		return activeID == "discover-a"
	})

	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "discover-b",
		Respawn:   true,
	})
	defer stopB()

	discoveredAt := time.Now()
	attachSess.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "discover-a") || !strings.Contains(row, "discover-b") {
			return fmt.Errorf("expected both headless tabs after new daemon start, row=%q", row)
		}
		return nil
	})
	if elapsed := time.Since(discoveredAt); elapsed > 2*time.Second {
		t.Fatalf("new headless tab discovery took too long: %v", elapsed)
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitUntilLocal(t, 4*time.Second, func() bool {
		activeMu.Lock()
		defer activeMu.Unlock()
		return activeID == "discover-b"
	})
}

func TestMultiAttachHeadlessRoutedStatusStaysOnActiveSession(t *testing.T) {
	dir := t.TempDir()
	socketA := dir + "/status-a.sock"
	socketB := dir + "/status-b.sock"
	stopA := startFakeLocalStatusServer(t, fakeLocalStatusServerSpec{
		SocketPath: socketA,
		SessionID:  "status-a",
		InitialWall: &attachStatusWall{
			Sender:  headless.RoutedStatusSenderLost,
			Message: "A_LOST connection lost to relay-a, reconnecting",
		},
		PeriodicWall: &attachStatusWall{
			Sender:  headless.RoutedStatusSenderLost,
			Message: "A_LOST connection lost to relay-a, reconnecting",
		},
		PeriodicEvery: 1500 * time.Millisecond,
	})
	defer stopA()
	stopB := startFakeLocalStatusServer(t, fakeLocalStatusServerSpec{
		SocketPath: socketB,
		SessionID:  "status-b",
		InitialWall: &attachStatusWall{
			Sender:  headless.RoutedStatusSenderLost,
			Message: "B_LOST connection lost to relay-b, reconnecting",
		},
		PeriodicWall: &attachStatusWall{
			Sender:  headless.RoutedStatusSenderBackoff,
			Message: "B_LOST connection lost to relay-b, reconnecting in 9s",
		},
		PeriodicEvery: 250 * time.Millisecond,
	})
	defer stopB()

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "status-a",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{
				{ID: "status-a", Name: "status-a", Status: "running", LastActiveAt: time.Now().UTC()},
				{ID: "status-b", Name: "status-b", Status: "running", LastActiveAt: time.Now().UTC()},
			}, nil
		},
		SocketResolver: func(sessionID string) (string, error) {
			switch sessionID {
			case "status-a":
				return socketA, nil
			case "status-b":
				return socketB, nil
			default:
				return "", fmt.Errorf("unknown session %q", sessionID)
			}
		},
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(20*time.Second, 150*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "A_LOST") {
			return fmt.Errorf("expected active routed status banner marker, got %q", row)
		}
		return nil
	})

	assertTopRowStatusStable(t, attachSess, 6*time.Second, "A_LOST", "B_LOST")

	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Eventually(20*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "B_LOST") {
			return fmt.Errorf("expected switched active routed status marker, got %q", row)
		}
		return nil
	})
	assertTopRowStatusStable(t, attachSess, 6*time.Second, "B_LOST", "A_LOST")
}

func TestMultiAttachHeadlessDoesNotForwardMouseReports(t *testing.T) {
	cfgDir := t.TempDir()
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "mouse-a",
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "mouse-a",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Wait(600 * time.Millisecond)
	attachSess.SendBytes([]byte("\x1b[<0;26;16M\x1b[<0;26;16m\n"))
	attachSess.Send("echo MOUSE_FILTER_OK\n")

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("MOUSE_FILTER_OK") {
			return fmt.Errorf("expected probe token output, row1=%q", screen.Row(1))
		}
		if screen.Contains("command not found") {
			return fmt.Errorf("unexpected mouse report leaked as command: %q", screen.String())
		}
		if screen.Contains("16M0;26;16m") {
			return fmt.Errorf("unexpected raw mouse report fragment leaked to shell: %q", screen.String())
		}
		return nil
	})
}

func TestMultiAttachHeadlessOfflineToggleForwarded(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "offline-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && offline
	})
	if attachSess.Screen().Contains("offline toggle is host local-only") {
		t.Fatalf("unexpected host-local-only banner after local offline toggle")
	}

	attachSess.SendCtrlL()
	attachSess.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && !offline
	})
	if attachSess.Screen().Contains("offline toggle is host local-only") {
		t.Fatalf("unexpected host-local-only banner after local offline untoggle")
	}
}

func TestMultiAttachHeadlessOfflineToggleUpdatesTabStyle(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "offline-style-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	if mode, ok := tabLabelMode(attachSess, sessionID); !ok {
		t.Fatalf("session label %q not visible", sessionID)
	} else if mode&terminal.ModeFaint != 0 || mode&terminal.ModeItalic != 0 {
		t.Fatalf("expected initial tab style not muted, mode=%d", mode)
	}

	attachSess.SendCtrlL()
	attachSess.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && offline
	})
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(_ ptytest.Screen) error {
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after offline toggle", sessionID)
		}
		if mode&terminal.ModeFaint == 0 || mode&terminal.ModeItalic == 0 {
			return fmt.Errorf("expected muted+italic tab style in offline mode, mode=%d", mode)
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && !offline
	})
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(_ ptytest.Screen) error {
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after offline untoggle", sessionID)
		}
		if mode&terminal.ModeFaint != 0 || mode&terminal.ModeItalic != 0 {
			return fmt.Errorf("expected non-muted tab style after offline untoggle, mode=%d", mode)
		}
		return nil
	})
}

func TestMultiAttachHeadlessOfflineToggleClearsRoutedStatusBanner(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "offline-banner-clear-a"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Respawn:   true,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	h.StopServer()
	attachSess.Eventually(12*time.Second, 150*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status before offline toggle, got row=%q", row)
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && offline
	})

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status cleared after offline toggle, row=%q", row)
		}
		return nil
	})
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(_ ptytest.Screen) error {
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after offline toggle", sessionID)
		}
		if mode&terminal.ModeFaint == 0 || mode&terminal.ModeItalic == 0 {
			return fmt.Errorf("expected muted+italic tab style in offline mode, mode=%d", mode)
		}
		return nil
	})
	assertTopRowStatusStable(t, attachSess, 1200*time.Millisecond, "", "connection lost")
	assertTopRowStatusStable(t, attachSess, 1200*time.Millisecond, "", "reconnecting")
}

func TestMultiAttachHeadlessOfflineToggleImmediatelyMutesActiveTab(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "offline-immediate-style-a"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	h.StopServer()
	attachSess.Eventually(12*time.Second, 150*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status before offline toggle, got row=%q", row)
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("o")
	attachSess.Eventually(900*time.Millisecond, 60*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status cleared immediately, row=%q", row)
		}
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after immediate offline toggle", sessionID)
		}
		if mode&terminal.ModeFaint == 0 || mode&terminal.ModeItalic == 0 {
			return fmt.Errorf("expected immediate muted+italic tab style in offline mode, mode=%d", mode)
		}
		return nil
	})
}

func TestMultiAttachHeadlessOfflineToggleImmediateUIRoundTrip(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "offline-immediate-roundtrip-a"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	h.StopServer()
	attachSess.Eventually(12*time.Second, 150*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status before offline toggle, got row=%q", row)
		}
		return nil
	})

	// Offline: must clear reconnect status and mute/italicize tab immediately.
	attachSess.SendCtrlL()
	attachSess.Send("o")
	attachSess.Eventually(900*time.Millisecond, 60*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status cleared immediately, row=%q", row)
		}
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after immediate offline toggle", sessionID)
		}
		if mode&terminal.ModeFaint == 0 || mode&terminal.ModeItalic == 0 {
			return fmt.Errorf("expected immediate muted+italic tab style in offline mode, mode=%d", mode)
		}
		return nil
	})
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && offline
	})

	// Online again: must unmute tab immediately and reconnect banner must return.
	attachSess.SendCtrlL()
	attachSess.Send("o")
	attachSess.Eventually(900*time.Millisecond, 60*time.Millisecond, func(_ ptytest.Screen) error {
		mode, ok := tabLabelMode(attachSess, sessionID)
		if !ok {
			return fmt.Errorf("session label %q not visible after immediate online toggle", sessionID)
		}
		if mode&terminal.ModeFaint != 0 || mode&terminal.ModeItalic != 0 {
			return fmt.Errorf("expected immediate non-muted tab style in online mode, mode=%d", mode)
		}
		return nil
	})
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionID)
		return ok && !offline
	})
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status to return after online toggle, row=%q", row)
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLWForwardedToHostLogic(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "wall-local-a"
	var wallToggleCalls atomic.Int32
	h := newHarness(t,
		ptytest.WithClock(clock.New()),
		ptytest.WithRequestHook(func(r *http.Request) {
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/wall/inactivity") {
				wallToggleCalls.Add(1)
			}
		}),
	)
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("w")
	waitUntilLocal(t, 8*time.Second, func() bool {
		return wallToggleCalls.Load() > 0
	})
	attachSess.Eventually(4*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.String(), "wall inactivity requires authentication") {
			return fmt.Errorf("unexpected authentication requirement from local ctrl+l w forwarding")
		}
		if strings.Contains(screen.String(), "wall inactivity toggle failed") {
			return fmt.Errorf("unexpected toggle failure after local ctrl+l w forwarding")
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLWOnlineShowsStatusCycle(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "wall-local-online-cycle"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Respawn:   true,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})
	waitUntilLocal(t, 8*time.Second, func() bool {
		return h.HasHost(sessionID)
	})

	requestControl := false
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
		RequestControl:     &requestControl,
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	sendAndExpect := func(expect string) {
		attachSess.SendCtrlL()
		attachSess.Send("w")
		attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
			row := screen.Row(0)
			if !strings.Contains(row, expect) {
				return fmt.Errorf("expected wall inactivity status %q, row=%q", expect, row)
			}
			return nil
		})
	}

	sendAndExpect("wall inactivity 2m")
	sendAndExpect("wall inactivity 5m")
	sendAndExpect("wall inactivity 15m")
	sendAndExpect("wall inactivity off")
}

func TestMultiAttachHeadlessCtrlLWShowsStatusBannerOfflineAndCyclesLevels(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "wall-local-cycle-offline"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	sendAndExpect := func(expect string) {
		attachSess.SendCtrlL()
		attachSess.Send("w")
		attachSess.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
			row := screen.Row(0)
			if !strings.Contains(row, expect) {
				return fmt.Errorf("expected wall inactivity status %q, row=%q", expect, row)
			}
			return nil
		})
	}

	sendAndExpect("wall inactivity 2m")
	sendAndExpect("wall inactivity 5m")
	sendAndExpect("wall inactivity 15m")
	sendAndExpect("wall inactivity off")
}

func TestMultiAttachHeadlessCtrlLWAfterOfflineToggleCyclesAndFansOutWithMockClock(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionA = "wall-offline-toggle-a"
	const sessionB = "wall-offline-toggle-b"

	h := newHarness(t)
	clk := h.Clock()
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionA,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Respawn:   true,
		Clock:     clk,
	})
	defer stopA()
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionB,
		Publish:   false,
		Respawn:   true,
		Clock:     clk,
	})
	defer stopB()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})
	waitUntilLocal(t, 8*time.Second, func() bool {
		return h.HasHost(sessionA)
	})

	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionA,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachA.Cancel)
	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionB,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachB.Cancel)

	attachA.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionA) {
			return fmt.Errorf("expected session a tab row, got %q", screen.Row(0))
		}
		return nil
	})
	attachB.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionB) {
			return fmt.Errorf("expected session b tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachA.SendCtrlL()
	attachA.Send("o")
	waitUntilLocal(t, 8*time.Second, func() bool {
		offline, ok := localOfflineState(cfgDir, sessionA)
		return ok && offline
	})
	attachA.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "offline mode on") {
			return fmt.Errorf("expected offline toggle status banner after ctrl+l o, row=%q", row)
		}
		return nil
	})

	sendAndExpect := func(expect string, verifyFanout bool) {
		attachA.SendCtrlL()
		attachA.Send("w")
		attachA.Eventually(5*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
			row := screen.Row(0)
			if !strings.Contains(row, expect) {
				return fmt.Errorf("expected wall inactivity status %q on source session, row=%q", expect, row)
			}
			return nil
		})
		if verifyFanout {
			attachB.Eventually(5*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
				row := screen.Row(0)
				if !strings.Contains(row, expect) {
					return fmt.Errorf("expected wall inactivity status %q fanout on peer session, row=%q", expect, row)
				}
				return nil
			})
		}
		h.Advance(2200 * time.Millisecond)
		attachA.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
			row := screen.Row(0)
			if strings.Contains(row, expect) {
				return fmt.Errorf("expected wall inactivity status %q to auto-hide, row=%q", expect, row)
			}
			return nil
		})
	}

	sendAndExpect("wall inactivity 2m", true)
	sendAndExpect("wall inactivity 5m", false)
	sendAndExpect("wall inactivity 15m", false)
	sendAndExpect("wall inactivity off", false)
}

func TestMultiAttachHeadlessCtrlLWFanoutToOtherLocalSessions(t *testing.T) {
	cfgDir := shortConfigDir(t)
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "wall-fanout-a",
		Publish:   false,
	})
	defer stopA()
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "wall-fanout-b",
		Publish:   false,
	})
	defer stopB()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "wall-fanout-a",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachA.Cancel)
	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "wall-fanout-b",
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachB.Cancel)

	attachA.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), "wall-fanout-a") {
			return fmt.Errorf("expected session a tab row, got %q", screen.Row(0))
		}
		return nil
	})
	attachB.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), "wall-fanout-b") {
			return fmt.Errorf("expected session b tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachA.SendCtrlL()
	attachA.Send("w")

	attachB.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "wall inactivity 2m") {
			return fmt.Errorf("expected wall inactivity status fanout on session b, row=%q", row)
		}
		return nil
	})
}

func TestMultiAttachHeadlessRelayWallPropagatesToOtherLocalSessions(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionA = "wall-prop-a"
	const sessionB = "wall-prop-b"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionA,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Respawn:   true,
	})
	defer stopA()
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionB,
		Publish:   false,
		Respawn:   true,
	})
	defer stopB()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})
	waitUntilLocal(t, 8*time.Second, func() bool {
		return h.HasHost(sessionA)
	})

	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionB,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachB.Cancel)

	attachB.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionB) {
			return fmt.Errorf("expected session b tab row, got %q", screen.Row(0))
		}
		return nil
	})

	wallMsg := "HEADLESS_RELAY_PROPAGATION_WALL"
	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), wallMsg, tlsDir, false); err != nil {
		t.Fatalf("SendWall: %v", err)
	}
	attachB.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(wallMsg) {
			return fmt.Errorf("expected relay wall propagated to local headless session b")
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLWForwardedWithAuthFileRefresh(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "wall-local-authfile"
	var wallToggleCalls atomic.Int32
	h := newHarness(t,
		ptytest.WithClock(clock.New()),
		ptytest.WithRequestHook(func(r *http.Request) {
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/wall/inactivity") {
				wallToggleCalls.Add(1)
			}
		}),
	)
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		AuthFile:  h.AuthFile(),
		TLSDir:    filepath.Join(filepath.Dir(h.AuthFile()), "tls"),
		Respawn:   true,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("w")
	waitUntilLocal(t, 8*time.Second, func() bool {
		return wallToggleCalls.Load() > 0
	})
	attachSess.Eventually(4*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.String(), "wall inactivity requires authentication") {
			return fmt.Errorf("unexpected authentication requirement from local ctrl+l w forwarding")
		}
		if strings.Contains(screen.String(), "wall inactivity toggle failed") {
			return fmt.Errorf("unexpected toggle failure after local ctrl+l w forwarding")
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLWForwardedWithRequestControlDisabled(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "wall-local-noreqctl"
	var wallToggleCalls atomic.Int32
	h := newHarness(t,
		ptytest.WithClock(clock.New()),
		ptytest.WithRequestHook(func(r *http.Request) {
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/wall/inactivity") {
				wallToggleCalls.Add(1)
			}
		}),
	)
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Respawn:   true,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	requestControl := false
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
		RequestControl:     &requestControl,
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("w")
	waitUntilLocal(t, 8*time.Second, func() bool {
		return wallToggleCalls.Load() > 0
	})
	attachSess.Eventually(4*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.String(), "wall inactivity requires authentication") {
			return fmt.Errorf("unexpected authentication requirement from local ctrl+l w forwarding with request-control disabled")
		}
		if strings.Contains(screen.String(), "wall inactivity toggle failed") {
			return fmt.Errorf("unexpected toggle failure after local ctrl+l w forwarding with request-control disabled")
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLLClearsAndTabBarRecovers(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "clear-local-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Shell:     headlessInteractiveShell(t),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Send("printf 'CLEAR_SENTINEL\\n'\n")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("CLEAR_SENTINEL") {
			return fmt.Errorf("expected sentinel before clear")
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("l")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("CLEAR_SENTINEL") {
			return fmt.Errorf("expected sentinel to be cleared from viewport, still present")
		}
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected tab bar visible after ctrl+l l clear, row=%q", screen.Row(0))
		}
		return nil
	})

	attachSess.Send("echo TABBAR_AFTER_CLEAR\n")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("TABBAR_AFTER_CLEAR") {
			return fmt.Errorf("expected post-clear command output")
		}
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected tab bar to recover after clear flow, row=%q", screen.Row(0))
		}
		return nil
	})
}

func TestMultiAttachHeadlessGraceExit(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "exit-local-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Respawn:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.Send("exit\n")
	attachSess.Eventually(2*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected retained session tab during grace, got %q", screen.Row(0))
		}
		return nil
	})
	if exited, err := attachSess.WaitErr(0); exited {
		t.Fatalf("attach exited too early during grace: %v", err)
	}

	h.Advance(6 * time.Second)

	var runErr error
	waitUntil(t, h.Clock(), 2*time.Second, func() bool {
		if ok, err := attachSess.WaitErr(0); ok {
			runErr = err
			return true
		}
		return false
	})
	if runErr != nil && runErr != context.Canceled && !strings.Contains(runErr.Error(), "no sessions available") {
		t.Fatalf("unexpected attach exit error: %v", runErr)
	}
}

func TestMultiAttachHeadlessRoutesRealPublishStatusBanner(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "status-live-a"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	h.StopServer()
	attachSess.Eventually(12*time.Second, 150*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected routed disconnect status banner, got row=%q", row)
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLRTogglesRespawnAndSurvivesExit(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "respawn-local-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Respawn:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("r")
	attachSess.Wait(200 * time.Millisecond)

	attachSess.Send("exit\n")
	if done, err := attachSess.WaitErr(2 * time.Second); done {
		t.Fatalf("expected attach to remain alive after exit with respawn enabled, err=%v", err)
	}

	attachSess.Send("echo RESPAWN_OK\n")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("RESPAWN_OK") {
			return fmt.Errorf("expected command to run after respawn")
		}
		return nil
	})
}

func TestMultiAttachHeadlessCtrlLCDoesNotCreateSession(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "ctrl-l-c-headless-noop-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) == 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.Send("X_HEADLESS_CTRL_L_C=sticky\n")
	attachSess.Send("echo __X_BEFORE__$X_HEADLESS_CTRL_L_C\n")
	attachSess.Eventually(6*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("__X_BEFORE__sticky") {
			return fmt.Errorf("expected shell marker before ctrl+l c")
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("c")
	attachSess.Wait(300 * time.Millisecond)
	attachSess.Send("echo __X_AFTER__$X_HEADLESS_CTRL_L_C\n")
	attachSess.Eventually(6*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("__X_AFTER__sticky") {
			return fmt.Errorf("ctrl+l c unexpectedly switched shell context; screen=%q", screen.String())
		}
		return nil
	})
}

func TestMultiAttachHeadlessReattachPreservesScrollbackPTY(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "rbuf-a"
	h := newHarness(t, ptytest.WithClock(clock.New()))
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})

	attachA.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachA.Send("i=1; while [ $i -le 140 ]; do printf 'RBUF-%03d\\n' $i; i=$((i+1)); done\n")
	attachA.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("RBUF-140") {
			return fmt.Errorf("expected newest scrollback probe row, row1=%q", screen.Row(1))
		}
		return nil
	})
	attachA.Cancel()
	if done, err := attachA.WaitErr(4 * time.Second); !done {
		t.Fatalf("first attach did not exit after cancel")
	} else if err != nil && err != context.Canceled {
		t.Fatalf("first attach wait err: %v", err)
	}

	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               120,
		Rows:               30,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachB.Cancel)

	attachB.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row after reattach, got %q", screen.Row(0))
		}
		return nil
	})

	attachB.SendCtrlL()
	attachB.Send("[")

	found := false
	for i := 0; i < 12; i++ {
		if attachB.Screen().Contains("RBUF-001") {
			found = true
			break
		}
		attachB.SendBytes([]byte{0x1b, '[', '5', '~'})
		attachB.Wait(90 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected reattach scrollback to include earliest rows; got:\n%s", attachB.Screen().String())
	}
}

func TestMultiAttachHeadlessResizePropagatesToPTY(t *testing.T) {
	cfgDir := t.TempDir()
	const sessionID = "resize-local-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               80,
		Rows:               24,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.Resize(110, 35)
	attachSess.Send("printf '__SIZE__%s\\n' \"$(stty size)\"\n")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("__SIZE__35 110") {
			return fmt.Errorf("expected resized PTY dimensions in shell output, got row1=%q", screen.Row(1))
		}
		return nil
	})
}

func TestMultiAttachHeadlessInitialAttachSizePropagatesToPTY(t *testing.T) {
	cfgDir := shortConfigDir(t)
	const sessionID = "resize-initial-local-a"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               101,
		Rows:               33,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(0), sessionID) {
			return fmt.Errorf("expected session id on tab row, got %q", screen.Row(0))
		}
		return nil
	})

	attachSess.Send("printf '__SIZE_INIT__%s\\n' \"$(stty size)\"\n")
	attachSess.Eventually(8*time.Second, 120*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("__SIZE_INIT__33 101") {
			return fmt.Errorf("expected initial attach PTY dimensions, got row1=%q", screen.Row(1))
		}
		return nil
	})
}

type headlessDaemonSpec struct {
	ConfigDir string
	SessionID string
	Publish   bool
	Endpoint  string
	Token     string
	AuthFile  string
	TLSDir    string
	Shell     string
	Respawn   bool
	Clock     clock.Clock
}

func startHeadlessDaemon(t *testing.T, spec headlessDaemonSpec) func() {
	t.Helper()
	shellPath := strings.TrimSpace(spec.Shell)
	if shellPath == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shellPath = "/bin/bash"
		} else {
			shellPath = "/bin/sh"
		}
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	d := headlessd.New(headlessd.Options{
		ConfigDir: spec.ConfigDir,
		SessionID: spec.SessionID,
		Publish:   spec.Publish,
		Endpoint:  spec.Endpoint,
		Token:     spec.Token,
		AuthFile:  spec.AuthFile,
		TLSDir:    spec.TLSDir,
		Shell:     shellPath,
		Respawn:   spec.Respawn,
		Clock:     spec.Clock,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	socketPath, err := headless.SocketPath(spec.ConfigDir, spec.SessionID)
	if err != nil {
		t.Fatalf("SocketPath(%q): %v", spec.SessionID, err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-runErr:
			if err != nil && err != context.Canceled {
				t.Fatalf("daemon %s failed before socket ready: %v", spec.SessionID, err)
			}
			t.Fatalf("daemon %s exited before socket ready", spec.SessionID)
		default:
		}
		if headless.SocketExists(socketPath) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !headless.SocketExists(socketPath) {
		t.Fatalf("daemon %s socket not ready: %s", spec.SessionID, socketPath)
	}

	return func() {
		cancelRun()
		select {
		case err := <-runErr:
			if err != nil && err != context.Canceled {
				t.Fatalf("daemon %s run: %v", spec.SessionID, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("daemon %s did not stop", spec.SessionID)
		}
	}
}

func headlessInteractiveShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "headless-interactive-shell.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

prompt='PROMPT> '
line=''

draw_prompt() {
  printf '%s' "$prompt"
}

redraw_line() {
  printf '\r\033[2K'
  draw_prompt
  printf '%s' "$line"
}

run_line() {
  printf '\r\n'
  if [ -n "$line" ]; then
    eval "$line"
  fi
  line=''
  draw_prompt
}

clear_screen() {
  printf '\033[H\033[2J'
  line=''
  draw_prompt
}

draw_prompt
while IFS= read -rsn1 ch; do
  if [ -z "$ch" ]; then
    run_line
    continue
  fi
  case "$ch" in
    $'\f')
      clear_screen
      ;;
    $'\r'|$'\n')
      run_line
      ;;
    $'\177'|$'\b')
      if [ -n "$line" ]; then
        line="${line%?}"
        redraw_line
      fi
      ;;
    *)
      line+="$ch"
      printf '%s' "$ch"
      ;;
  esac
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write interactive shell wrapper: %v", err)
	}
	return scriptPath
}

func localHeadlessSessionSource(cfgDir string) func(context.Context) ([]attach.SessionInfo, error) {
	return func(context.Context) ([]attach.SessionInfo, error) {
		store := headless.NewStore(cfgDir)
		records, err := store.Reconcile()
		if err != nil {
			return nil, err
		}
		out := make([]attach.SessionInfo, 0, len(records))
		for _, rec := range records {
			out = append(out, attach.SessionInfo{
				ID:           rec.SessionID,
				Name:         rec.SessionID,
				Status:       rec.Status,
				Offline:      rec.Offline,
				LastActiveAt: rec.StartedAt,
			})
		}
		return out, nil
	}
}

func startHeadlessStateWatcher(t *testing.T, ctx context.Context, cfgDir string) (<-chan struct{}, func()) {
	t.Helper()
	events, stopFn, err := headless.StartStateWatcher(ctx, cfgDir)
	if err != nil {
		t.Fatalf("StartStateWatcher: %v", err)
	}
	return events, func() {
		if err := stopFn(); err != nil {
			t.Fatalf("stop state watcher: %v", err)
		}
	}
}

func localHeadlessSocketResolver(cfgDir string) func(string) (string, error) {
	return func(sessionID string) (string, error) {
		store := headless.NewStore(cfgDir)
		records, err := store.Reconcile()
		if err != nil {
			return "", err
		}
		for _, rec := range records {
			if rec.SessionID == sessionID {
				return rec.SocketPath, nil
			}
		}
		return "", fmt.Errorf("session %q not found", sessionID)
	}
}

type attachStatusWall struct {
	Sender  string
	Message string
}

type fakeLocalStatusServerSpec struct {
	SocketPath    string
	SessionID     string
	InitialWall   *attachStatusWall
	PeriodicWall  *attachStatusWall
	PeriodicEvery time.Duration
}

func startFakeLocalStatusServer(t *testing.T, spec fakeLocalStatusServerSpec) func() {
	t.Helper()
	if strings.TrimSpace(spec.SocketPath) == "" {
		t.Fatalf("socket path is required")
	}
	if strings.TrimSpace(spec.SessionID) == "" {
		t.Fatalf("session id is required")
	}
	_ = os.Remove(spec.SocketPath)
	ln, err := net.Listen("unix", spec.SocketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	_ = os.Chmod(spec.SocketPath, 0o600)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/client", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() {
			_ = ws.Close(websocket.StatusNormalClosure, "bye")
		}()
		if _, err := readProtoFrame(r.Context(), ws); err != nil {
			return
		}
		if err := writeProtoFrame(r.Context(), ws, &protocolpb.Frame{
			SessionId: spec.SessionID,
			Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{
				GrantedControl: true,
				ServerCols:     120,
				ServerRows:     30,
				HolderClientId: "local-test",
			}},
		}); err != nil {
			return
		}
		if err := writeProtoFrame(r.Context(), ws, &protocolpb.Frame{
			SessionId: spec.SessionID,
			Payload:   &protocolpb.Frame_Snapshot{Snapshot: blankSnapshot(120, 30)},
		}); err != nil {
			return
		}
		if spec.InitialWall != nil {
			_ = writeProtoFrame(r.Context(), ws, &protocolpb.Frame{
				SessionId: spec.SessionID,
				Payload: &protocolpb.Frame_Wall{Wall: &protocolpb.Wall{
					Sender:  spec.InitialWall.Sender,
					Message: spec.InitialWall.Message,
				}},
			})
		}
		go func() {
			for {
				if _, err := readProtoFrame(r.Context(), ws); err != nil {
					return
				}
			}
		}()
		if spec.PeriodicWall != nil && spec.PeriodicEvery > 0 {
			ticker := time.NewTicker(spec.PeriodicEvery)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if err := writeProtoFrame(r.Context(), ws, &protocolpb.Frame{
						SessionId: spec.SessionID,
						Payload: &protocolpb.Frame_Wall{Wall: &protocolpb.Wall{
							Sender:  spec.PeriodicWall.Sender,
							Message: spec.PeriodicWall.Message,
						}},
					}); err != nil {
						return
					}
				}
			}
		}
		<-r.Context().Done()
	})
	server := &http.Server{Handler: mux}
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Serve(ln)
	}()
	return func() {
		_ = server.Shutdown(context.Background())
		_ = ln.Close()
		_ = os.Remove(spec.SocketPath)
		select {
		case <-runErr:
		case <-time.After(2 * time.Second):
			t.Fatalf("fake local status server did not stop")
		}
	}
}

func readProtoFrame(ctx context.Context, ws *websocket.Conn) (*protocolpb.Frame, error) {
	_, payload, err := ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(payload, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

func writeProtoFrame(ctx context.Context, ws *websocket.Conn, frame *protocolpb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return ws.Write(ctx, websocket.MessageBinary, data)
}

func blankSnapshot(cols, rows int) *protocolpb.Snapshot {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	size := cols * rows
	runes := make([]uint32, size)
	for i := range runes {
		runes[i] = ' '
	}
	return &protocolpb.Snapshot{
		Cols:   uint32(cols),
		Rows:   uint32(rows),
		Runes:  runes,
		Fg:     make([]uint32, size),
		Bg:     make([]uint32, size),
		Cursor: &protocolpb.Cursor{X: 0, Y: 0},
	}
}

func localSessionCount(cfgDir string) int {
	store := headless.NewStore(cfgDir)
	records, err := store.Reconcile()
	if err != nil {
		return 0
	}
	return len(records)
}

func localOfflineState(cfgDir, sessionID string) (bool, bool) {
	store := headless.NewStore(cfgDir)
	records, err := store.Reconcile()
	if err != nil {
		return false, false
	}
	for _, rec := range records {
		if rec.SessionID == sessionID {
			return rec.Offline, true
		}
	}
	return false, false
}

func tabLabelMode(sess *ptytest.PTYSession, label string) (int16, bool) {
	row := sess.Screen().Row(0)
	col := strings.Index(row, label)
	if col < 0 {
		return 0, false
	}
	cell, ok := sess.CellAt(1, col+1)
	if !ok {
		return 0, false
	}
	return cell.Mode, true
}

func waitUntilLocal(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out after %v", timeout)
}

func shortConfigDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lingon-headless-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func assertTopRowStatusStable(t *testing.T, sess *ptytest.PTYSession, duration time.Duration, want, forbid string) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		row := sess.Screen().Row(0)
		if forbid != "" && strings.Contains(row, forbid) {
			t.Fatalf("inactive routed status leaked onto active row 1: %q", row)
		}
		if want != "" && !strings.Contains(row, want) {
			t.Fatalf("active routed status flickered off on row 1: %q", row)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
