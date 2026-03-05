//go:build !windows

package headless

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStateWatcherSignalsSignificantSessionChanges(t *testing.T) {
	cfgDir := shortWatcherConfigDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, stopWatcher, err := StartStateWatcher(ctx, cfgDir)
	if err != nil {
		t.Fatalf("StartStateWatcher: %v", err)
	}
	defer func() {
		_ = stopWatcher()
	}()

	store := NewStore(cfgDir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["watch-a"] = SessionRecord{
			SessionID:  "watch-a",
			PID:        os.Getpid(),
			SocketPath: "/tmp/watch-a.sock",
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("add session: %v", err)
	}
	waitForWatcherEvent(t, events, 2*time.Second)
	drainWatcherEvents(events)

	if err := store.WithLock(func(state *State) error {
		rec := state.Sessions["watch-a"]
		rec.LastSeenAt = now.Add(2 * time.Second)
		state.Sessions["watch-a"] = rec
		return nil
	}); err != nil {
		t.Fatalf("heartbeat update: %v", err)
	}
	assertNoWatcherEvent(t, events, 350*time.Millisecond)

	if err := store.WithLock(func(state *State) error {
		rec := state.Sessions["watch-a"]
		rec.Offline = true
		rec.Status = "offline"
		state.Sessions["watch-a"] = rec
		return nil
	}); err != nil {
		t.Fatalf("offline toggle: %v", err)
	}
	waitForWatcherEvent(t, events, 2*time.Second)
	drainWatcherEvents(events)

	if err := store.WithLock(func(state *State) error {
		delete(state.Sessions, "watch-a")
		return nil
	}); err != nil {
		t.Fatalf("remove session: %v", err)
	}
	waitForWatcherEvent(t, events, 2*time.Second)
}

func TestStateWatcherStopRemovesWatcherRecord(t *testing.T) {
	cfgDir := shortWatcherConfigDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, stopWatcher, err := StartStateWatcher(ctx, cfgDir)
	if err != nil {
		t.Fatalf("StartStateWatcher: %v", err)
	}
	store := NewStore(cfgDir)
	assertWatcherCount(t, store, 1)

	if err := stopWatcher(); err != nil {
		t.Fatalf("stop watcher: %v", err)
	}
	assertWatcherCount(t, store, 0)
}

func waitForWatcherEvent(t *testing.T, events <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-events:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for watcher event after %v", timeout)
	}
}

func assertNoWatcherEvent(t *testing.T, events <-chan struct{}, d time.Duration) {
	t.Helper()
	select {
	case <-events:
		t.Fatalf("unexpected watcher event within %v", d)
	case <-time.After(d):
	}
}

func drainWatcherEvents(events <-chan struct{}) {
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

func assertWatcherCount(t *testing.T, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := store.Load()
		if err == nil && len(state.Watchers) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Fatalf("watcher count = %d, want %d", len(state.Watchers), want)
}

func shortWatcherConfigDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lingon-watch-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
