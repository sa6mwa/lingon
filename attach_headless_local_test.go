package lingon

import (
	"context"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/headless"
)

func TestHeadlessSessionSource(t *testing.T) {
	cfgDir := t.TempDir()
	store := headless.NewStore(cfgDir)
	now := time.Now().UTC()
	err := store.WithLock(func(state *headless.State) error {
		state.Sessions["a"] = headless.SessionRecord{
			SessionID:  "a",
			PID:        1,
			SocketPath: "/tmp/a.sock",
			LastSeenAt: now.Add(-time.Minute),
			Status:     "running",
		}
		state.Sessions["b"] = headless.SessionRecord{
			SessionID:  "b",
			PID:        2,
			SocketPath: "/tmp/b.sock",
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
	store := headless.NewStore(cfgDir)
	err := store.WithLock(func(state *headless.State) error {
		state.Sessions["abc"] = headless.SessionRecord{
			SessionID:  "abc",
			PID:        1,
			SocketPath: "/tmp/abc.sock",
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
	if socketPath != "/tmp/abc.sock" {
		t.Fatalf("resolved socket path = %q, want %q", socketPath, "/tmp/abc.sock")
	}

	_, err = headlessSocketResolver(cfgDir)("missing")
	if err == nil {
		t.Fatalf("expected missing session error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}
