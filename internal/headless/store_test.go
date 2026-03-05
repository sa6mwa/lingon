package headless

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeSessionID(t *testing.T) {
	got, err := NormalizeSessionID(" prod/shell ")
	if err != nil {
		t.Fatalf("NormalizeSessionID: %v", err)
	}
	if got != "prod-shell" {
		t.Fatalf("NormalizeSessionID = %q, want %q", got, "prod-shell")
	}
}

func TestStoreLoadMissingReturnsEmptyState(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.Version == 0 {
		t.Fatalf("Version is zero")
	}
	if len(state.Sessions) != 0 {
		t.Fatalf("sessions len = %d, want 0", len(state.Sessions))
	}
}

func TestStoreWithLockPersistsUpdates(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	now := time.Now().UTC().Round(time.Second)
	if err := store.WithLock(func(state *State) error {
		state.Sessions["s1"] = SessionRecord{
			SessionID:  "s1",
			PID:        1234,
			SocketPath: filepath.Join(BaseDir(dir), "s1.sock"),
			StartedAt:  now,
			LastSeenAt: now,
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rec, ok := loaded.Sessions["s1"]
	if !ok {
		t.Fatalf("missing session s1")
	}
	if rec.PID != 1234 {
		t.Fatalf("PID = %d, want 1234", rec.PID)
	}
}
