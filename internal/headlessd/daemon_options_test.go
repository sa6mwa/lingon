package headlessd

import (
	"os"
	"reflect"
	"testing"
	"time"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/headless"
)

func TestNormalizeWallInactiveAfterLevelsDefaultsWhenEmpty(t *testing.T) {
	got := normalizeWallInactiveAfterLevels(nil)
	want := config.DefaultWallInactiveAfterLevels()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestNormalizeWallInactiveAfterLevelsFiltersInvalidAndDedupes(t *testing.T) {
	got := normalizeWallInactiveAfterLevels([]time.Duration{
		0,
		2 * time.Minute,
		2 * time.Minute,
		-1 * time.Second,
		5 * time.Minute,
	})
	want := []time.Duration{2 * time.Minute, 5 * time.Minute}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
}

func TestRemoveStateRecordSkipsDifferentPID(t *testing.T) {
	cfgDir := t.TempDir()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: "owned-by-other",
	})
	d.sessionID = "owned-by-other"
	foreignPID := os.Getpid() + 1

	if err := d.store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions[d.sessionID] = headless.SessionRecord{
			SessionID:  d.sessionID,
			PID:        foreignPID,
			SocketPath: "/tmp/owned-by-other.sock",
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	if err := d.removeStateRecord(); err != nil {
		t.Fatalf("removeStateRecord: %v", err)
	}
	state, err := d.store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if _, ok := state.Sessions[d.sessionID]; !ok {
		t.Fatalf("expected foreign-owned session record to remain")
	}
}

func TestRemoveStateRecordDeletesOwnedPID(t *testing.T) {
	cfgDir := t.TempDir()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: "owned-by-self",
	})
	d.sessionID = "owned-by-self"

	if err := d.store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions[d.sessionID] = headless.SessionRecord{
			SessionID:  d.sessionID,
			PID:        os.Getpid(),
			SocketPath: "/tmp/owned-by-self.sock",
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	if err := d.removeStateRecord(); err != nil {
		t.Fatalf("removeStateRecord: %v", err)
	}
	state, err := d.store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if _, ok := state.Sessions[d.sessionID]; ok {
		t.Fatalf("expected owned session record to be removed")
	}
}
