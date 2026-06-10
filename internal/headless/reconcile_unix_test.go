//go:build !windows

package headless

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReconcilePrunesDeadPIDEvenWhenSocketFileRemains(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(BaseDir(dir), "stale.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: socketPath}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("bind unix socket: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close socket: %v", err)
	}
	if !SocketExists(socketPath) {
		t.Fatalf("expected orphan socket inode at %s", socketPath)
	}

	store := NewStore(dir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["stale"] = SessionRecord{
			SessionID:  "stale",
			PID:        999999999,
			SocketPath: socketPath,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	records, err := store.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records len = %d, want 0", len(records))
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Sessions["stale"]; ok {
		t.Fatalf("stale session remained in state")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("orphan socket was not removed: %v", err)
	}
}

func TestReconcileKeepsNoPIDRecordWithSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(BaseDir(dir), "legacy.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	store := NewStore(dir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["legacy"] = SessionRecord{
			SessionID:  "legacy",
			PID:        -1,
			SocketPath: socketPath,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	records, err := store.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(records) != 1 || records[0].SessionID != "legacy" {
		t.Fatalf("records = %+v, want legacy session", records)
	}
}
