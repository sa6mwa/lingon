//go:build !windows

package headless

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestReconcileKeepsLivePIDWithoutReachableSocket(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["live"] = SessionRecord{
			SessionID:  "live",
			PID:        cmd.Process.Pid,
			SocketPath: filepath.Join(BaseDir(dir), "missing.sock"),
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
	if len(records) != 1 || records[0].SessionID != "live" {
		t.Fatalf("records = %+v, want live session", records)
	}
}

func TestDetachWithUnreachableSocketDoesNotKillPIDOnlyRecord(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["stale-pid"] = SessionRecord{
			SessionID:  "stale-pid",
			PID:        cmd.Process.Pid,
			SocketPath: filepath.Join(BaseDir(dir), "missing.sock"),
			StartedAt:  now.Add(-time.Hour),
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	if err := DetachSession(context.Background(), dir, "stale-pid"); err != nil {
		t.Fatalf("DetachSession: %v", err)
	}
	if !PIDAlive(cmd.Process.Pid) {
		t.Fatalf("detach killed process identified only by stale pid")
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Sessions["stale-pid"]; ok {
		t.Fatalf("stale session remained in state")
	}
}

func TestDetachDoesNotRemoveSocketOutsideHeadlessDir(t *testing.T) {
	dir := t.TempDir()
	outsideSocket := filepath.Join(t.TempDir(), "outside.sock")
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: outsideSocket}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("bind unix socket: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close socket: %v", err)
	}

	store := NewStore(dir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["outside"] = SessionRecord{
			SessionID:  "outside",
			PID:        -1,
			SocketPath: outsideSocket,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	if err := DetachSession(context.Background(), dir, "outside"); err != nil {
		t.Fatalf("DetachSession: %v", err)
	}
	if !SocketExists(outsideSocket) {
		t.Fatalf("detach removed socket outside headless dir")
	}
}

func TestDetachWithUnreachableSocketKillsMatchingPIDRecord(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process start-time identity check is linux-only")
	}
	dir := t.TempDir()
	store := NewStore(dir)
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	if !ProcessMatchesStartTime(cmd.Process.Pid, time.Now().UTC()) {
		t.Fatalf("test process did not match current start time")
	}

	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["wedged"] = SessionRecord{
			SessionID:  "wedged",
			PID:        cmd.Process.Pid,
			SocketPath: filepath.Join(BaseDir(dir), "missing.sock"),
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	if err := DetachSession(context.Background(), dir, "wedged"); err != nil {
		t.Fatalf("DetachSession: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	select {
	case <-waitCh:
	case <-time.After(time.Second):
		t.Fatalf("detach did not stop matching pid record")
	}
}

func TestReconcilePrunesLivePIDWithMismatchedStartTime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process start-time identity check is linux-only")
	}
	dir := t.TempDir()
	store := NewStore(dir)
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["reused"] = SessionRecord{
			SessionID:  "reused",
			PID:        cmd.Process.Pid,
			SocketPath: filepath.Join(BaseDir(dir), "missing.sock"),
			StartedAt:  now.Add(-time.Hour),
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
	if !PIDAlive(cmd.Process.Pid) {
		t.Fatalf("reconcile killed process while pruning stale record")
	}
}

func TestReconcileDoesNotRemoveStaleRecordPathOutsideHeadlessDir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(outside, []byte("do not delete"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := NewStore(dir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["stale"] = SessionRecord{
			SessionID:  "stale",
			PID:        999999999,
			SocketPath: outside,
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
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was removed or changed: %v", err)
	}
}

func TestReconcileDoesNotRemoveSocketOutsideHeadlessDir(t *testing.T) {
	dir := t.TempDir()
	outsideSocket := filepath.Join(t.TempDir(), "outside.sock")
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: outsideSocket}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("bind unix socket: %v", err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close socket: %v", err)
	}
	if !SocketExists(outsideSocket) {
		t.Fatalf("expected outside socket inode at %s", outsideSocket)
	}

	store := NewStore(dir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Sessions["stale"] = SessionRecord{
			SessionID:  "stale",
			PID:        999999999,
			SocketPath: outsideSocket,
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
	if !SocketExists(outsideSocket) {
		t.Fatalf("outside socket was removed")
	}
}
