package headlessd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/pslog"
)

func TestDaemonAttachAndSendViaUnixSocket(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-itest"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	var out bytes.Buffer
	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         &out,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	token := "HEADLESS_ITEST_TOKEN"
	if err := lingon.SendInput(context.Background(), lingon.SendInputOptions{
		Endpoint:       "local://headless",
		UnixSocket:     socketPath,
		SessionID:      sessionID,
		RequestControl: true,
		Tokens:         []string{"echo " + token},
		NoNewline:      false,
		Logger:         pslog.NoopLogger(),
	}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	waitUntil(t, 8*time.Second, func() bool {
		return strings.Contains(out.String(), token)
	})

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestDaemonRunRejectsDuplicateSessionID(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-duplicate-id"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	runCtxA, cancelRunA := context.WithCancel(context.Background())
	defer cancelRunA()
	daemonA := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErrA := make(chan error, 1)
	go func() {
		runErrA <- daemonA.Run(runCtxA)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	runCtxB, cancelRunB := context.WithCancel(context.Background())
	defer cancelRunB()
	daemonB := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErrB := make(chan error, 1)
	go func() {
		runErrB <- daemonB.Run(runCtxB)
	}()

	select {
	case err := <-runErrB:
		if err == nil {
			t.Fatalf("expected duplicate daemon start to fail")
		}
		if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("expected duplicate daemon error, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		cancelRunB()
		t.Fatalf("duplicate daemon start blocked instead of failing")
	}

	cancelRunA()
	select {
	case err := <-runErrA:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon A run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon A did not stop")
	}
}

func TestDaemonOfflineTogglePersistsState(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-offline"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
		t.Fatalf("send command toggle offline: %v", err)
	}

	store := headless.NewStore(cfgDir)
	waitUntil(t, 8*time.Second, func() bool {
		records, err := store.Reconcile()
		if err != nil {
			return false
		}
		for _, rec := range records {
			if rec.SessionID == sessionID {
				return rec.Offline
			}
		}
		return false
	})

	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestDaemonReplaysPublishStatusToNewAttachClient(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-status-replay"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	d.handlePublishStatus(session.PublishStatus{
		SessionID: sessionID,
		Kind:      session.PublishStatusConnectionLost,
		Message:   "connection lost to https://relay.example/v1, reconnecting",
		Endpoint:  "https://relay.example/v1",
	})

	var out bytes.Buffer
	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         &out,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	waitUntil(t, 8*time.Second, func() bool {
		return strings.Contains(out.String(), "connection lost to https://relay.example/v1, reconnecting")
	})

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestDaemonInactivityWallFiresOnceUntilNewActivity(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-inactive-once"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	clk := clock.NewMock()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir:               cfgDir,
		SessionID:               sessionID,
		Publish:                 false,
		Clock:                   clk,
		WallInactiveAfterLevels: []time.Duration{2 * time.Second},
		Logger:                  pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	var out bytes.Buffer
	var wallCount atomic.Int32
	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         &out,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
		OnWall: func(wall *protocolpb.Wall) {
			if wall == nil {
				return
			}
			if strings.Contains(strings.TrimSpace(wall.GetMessage()), "inactive") {
				wallCount.Add(1)
			}
		},
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY); err != nil {
		t.Fatalf("send command enable inactivity wall: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		return localWallState(d, func(enabled bool, after time.Duration, armed bool, _ time.Time) bool {
			return enabled && armed && after == 2*time.Second
		})
	})

	clk.Add(2500 * time.Millisecond)
	waitUntil(t, 2*time.Second, func() bool {
		return wallCount.Load() == 1
	})

	clk.Add(8 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := wallCount.Load(); got != 1 {
		t.Fatalf("expected one inactivity wall without new activity, got %d", got)
	}

	if err := lingon.SendInput(context.Background(), lingon.SendInputOptions{
		Endpoint:       "local://headless",
		UnixSocket:     socketPath,
		SessionID:      sessionID,
		RequestControl: true,
		Tokens:         []string{"echo REARM_INACTIVITY_WALL"},
		NoNewline:      false,
		Logger:         pslog.NoopLogger(),
	}); err != nil {
		t.Fatalf("SendInput activity: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		return strings.Contains(out.String(), "REARM_INACTIVITY_WALL")
	})
	waitUntil(t, 2*time.Second, func() bool {
		return localWallState(d, func(enabled bool, after time.Duration, armed bool, lastActivity time.Time) bool {
			return enabled && armed && after == 2*time.Second
		})
	})

	clk.Add(2500 * time.Millisecond)
	waitUntil(t, 2*time.Second, func() bool {
		return wallCount.Load() == 2
	})

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestDaemonModeSwitchDisablesLocalAndRelayWallInactivity(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "hd-mode-switch"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	var relayDisableCalls atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wall/inactivity":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				SessionID string `json:"session_id"`
				Enabled   *bool  `json:"enabled,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			enabled := false
			if req.Enabled != nil {
				enabled = *req.Enabled
			}
			if req.SessionID == sessionID && !enabled {
				relayDisableCalls.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id":     req.SessionID,
				"enabled":        enabled,
				"inactive_after": "",
			})
		case "/wall":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "sessions": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	clk := clock.NewMock()
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir:               cfgDir,
		SessionID:               sessionID,
		Publish:                 false,
		Endpoint:                relay.URL,
		Token:                   "test-token",
		Clock:                   clk,
		WallInactiveAfterLevels: []time.Duration{2 * time.Second},
		Logger:                  pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	var wallCount atomic.Int32
	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
		OnWall: func(wall *protocolpb.Wall) {
			if wall == nil {
				return
			}
			if strings.Contains(strings.TrimSpace(wall.GetMessage()), "inactive") {
				wallCount.Add(1)
			}
		},
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	// Offline + local inactivity enable.
	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
		t.Fatalf("send command offline toggle: %v", err)
	}
	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY); err != nil {
		t.Fatalf("send command local wall enable: %v", err)
	}
	clk.Add(2500 * time.Millisecond)
	waitUntil(t, 2*time.Second, func() bool {
		return wallCount.Load() == 1
	})

	// Online toggle must disable both local fallback and relay inactivity.
	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
		t.Fatalf("send command online toggle: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		return relayDisableCalls.Load() >= 2
	})
	if err := lingon.SendInput(context.Background(), lingon.SendInputOptions{
		Endpoint:       "local://headless",
		UnixSocket:     socketPath,
		SessionID:      sessionID,
		RequestControl: true,
		Tokens:         []string{"echo MODE_SWITCH_ACTIVITY"},
		NoNewline:      false,
		Logger:         pslog.NoopLogger(),
	}); err != nil {
		t.Fatalf("SendInput activity after online toggle: %v", err)
	}
	clk.Add(3500 * time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	if got := wallCount.Load(); got != 1 {
		t.Fatalf("expected local inactivity disabled after online toggle, got %d walls", got)
	}

	// Offline toggle should also leave local inactivity disabled until explicit re-enable.
	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
		t.Fatalf("send command offline toggle second: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		return relayDisableCalls.Load() >= 3
	})
	if err := lingon.SendInput(context.Background(), lingon.SendInputOptions{
		Endpoint:       "local://headless",
		UnixSocket:     socketPath,
		SessionID:      sessionID,
		RequestControl: true,
		Tokens:         []string{"echo MODE_SWITCH_ACTIVITY_2"},
		NoNewline:      false,
		Logger:         pslog.NoopLogger(),
	}); err != nil {
		t.Fatalf("SendInput activity after second offline toggle: %v", err)
	}
	clk.Add(3500 * time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	if got := wallCount.Load(); got != 1 {
		t.Fatalf("expected local inactivity still disabled after mode switch, got %d walls", got)
	}

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func TestDaemonOfflineInactivityWallPropagatesToRelay(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "hd-offline-relay"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	var relayWallCount atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wall":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Message string `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if strings.Contains(strings.TrimSpace(req.Message), "inactive") {
				relayWallCount.Add(1)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "sessions": 1})
		case "/wall/inactivity":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id":     sessionID,
				"enabled":        false,
				"inactive_after": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	clk := clock.NewMock()
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir:               cfgDir,
		SessionID:               sessionID,
		Publish:                 false,
		Endpoint:                relay.URL,
		Token:                   "test-token",
		Clock:                   clk,
		WallInactiveAfterLevels: []time.Duration{2 * time.Second},
		Logger:                  pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	var localWallCount atomic.Int32
	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
		OnWall: func(wall *protocolpb.Wall) {
			if wall == nil {
				return
			}
			if strings.Contains(strings.TrimSpace(wall.GetMessage()), "inactive") {
				localWallCount.Add(1)
			}
		},
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
		t.Fatalf("send command offline toggle: %v", err)
	}
	if err := sendHeadlessCommand(context.Background(), socketPath, sessionID, protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY); err != nil {
		t.Fatalf("send command local wall enable: %v", err)
	}

	clk.Add(2500 * time.Millisecond)
	waitUntil(t, 2*time.Second, func() bool {
		return localWallCount.Load() == 1
	})
	waitUntil(t, 2*time.Second, func() bool {
		return relayWallCount.Load() >= 1
	})

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}

func sendHeadlessCommand(ctx context.Context, socketPath, sessionID string, kind protocolpb.CommandKind) error {
	client := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		NoHostTimeout:  5 * time.Second,
		Logger:         pslog.NoopLogger(),
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan struct{})
	client.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.RunDetached(runCtx)
	}()
	select {
	case <-ready:
	case err := <-errCh:
		if err != nil {
			return err
		}
		return fmt.Errorf("attach exited before command send")
	case <-time.After(5 * time.Second):
		return fmt.Errorf("attach not ready for command send")
	}
	if err := client.SendCommand(ctx, kind); err != nil {
		client.Close("command send failed")
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
		return err
	}
	client.Close("command sent")
	select {
	case err := <-errCh:
		if err != nil &&
			!errors.Is(err, context.Canceled) &&
			!strings.Contains(err.Error(), "command sent") {
			return err
		}
	case <-time.After(2 * time.Second):
	}
	return nil
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
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

func localWallState(d *Daemon, fn func(enabled bool, after time.Duration, armed bool, lastActivity time.Time) bool) bool {
	d.wallMu.Lock()
	defer d.wallMu.Unlock()
	return fn(d.wallEnabled, d.wallAfter, d.wallArmed, d.wallLastActivity)
}
