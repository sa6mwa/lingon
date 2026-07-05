package lingon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/headless"
)

func TestHeadlessSessionSource(t *testing.T) {
	cfgDir := t.TempDir()
	socketA := listenHeadlessSocket(t, cfgDir, "a")
	socketB := listenHeadlessSocket(t, cfgDir, "b")
	store := headless.NewStore(cfgDir)
	now := time.Now().UTC()
	err := store.WithLock(func(state *headless.State) error {
		state.Sessions["a"] = headless.SessionRecord{
			SessionID:  "a",
			PID:        os.Getpid(),
			SocketPath: socketA,
			LastSeenAt: now.Add(-time.Minute),
			Status:     "running",
		}
		state.Sessions["b"] = headless.SessionRecord{
			SessionID:  "b",
			PID:        os.Getpid(),
			SocketPath: socketB,
			LastSeenAt: now,
			Status:     "running",
			Offline:    true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed state: %v", err)
	}

	sessions, err := headlessSessionSource(cfgDir)(context.Background())
	if err != nil {
		t.Fatalf("headlessSessionSource: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	names := map[string]string{}
	offline := map[string]bool{}
	for _, session := range sessions {
		names[session.ID] = session.Name
		offline[session.ID] = session.Offline
	}
	if sessions[0].ID != "a" || sessions[1].ID != "b" {
		t.Fatalf("expected headless sessions sorted by id, got %q, %q", sessions[0].ID, sessions[1].ID)
	}
	if names["a"] != "a" {
		t.Fatalf("expected session a name to match id, got %q", names["a"])
	}
	if names["b"] != "b" {
		t.Fatalf("expected session b name to match id, got %q", names["b"])
	}
	if offline["a"] {
		t.Fatalf("expected session a offline=false")
	}
	if !offline["b"] {
		t.Fatalf("expected session b offline=true")
	}
}

func TestHeadlessSocketResolver(t *testing.T) {
	cfgDir := t.TempDir()
	expectedSocketPath := listenHeadlessSocket(t, cfgDir, "abc")
	store := headless.NewStore(cfgDir)
	err := store.WithLock(func(state *headless.State) error {
		state.Sessions["abc"] = headless.SessionRecord{
			SessionID:  "abc",
			PID:        os.Getpid(),
			SocketPath: expectedSocketPath,
			Status:     "running",
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed state: %v", err)
	}

	socketPath, err := headlessSocketResolver(cfgDir)("abc")
	if err != nil {
		t.Fatalf("resolve existing socket: %v", err)
	}
	if got, want := socketPath, expectedSocketPath; got != want {
		t.Fatalf("resolved socket path = %q, want %q", got, want)
	}

	_, err = headlessSocketResolver(cfgDir)("missing")
	if err == nil {
		t.Fatalf("expected missing session error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func listenHeadlessSocket(t *testing.T, cfgDir, sessionID string) string {
	t.Helper()
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})
	return socketPath
}
