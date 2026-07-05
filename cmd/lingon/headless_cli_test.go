package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/testutil"
)

func TestSessionsHeadlessUsesLocalState(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	socketPath := listenTestHeadlessSocket(t, cfgDir, "local-a")
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		state.Sessions["local-a"] = headless.SessionRecord{
			SessionID:  "local-a",
			PID:        os.Getpid(),
			SocketPath: socketPath,
			StartedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"sessions", "-x"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "\"id\": \"local-a\"") {
		t.Fatalf("expected local session in output, got: %s", out.String())
	}
}

func TestDetachRemovesLocalState(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	socketPath, err := headless.SocketPath(cfgDir, "local-b")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if err := os.MkdirAll(headless.BaseDir(cfgDir), 0o700); err != nil {
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
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		state.Sessions["local-b"] = headless.SessionRecord{
			SessionID:  "local-b",
			PID:        -1,
			SocketPath: socketPath,
			StartedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"detach", "local-b"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Sessions["local-b"]; ok {
		t.Fatalf("session still present after detach")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path still exists")
	}
}

func TestDetachAllRemovesAllLocalState(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	if err := os.MkdirAll(headless.BaseDir(cfgDir), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	socketA, err := headless.SocketPath(cfgDir, "local-a")
	if err != nil {
		t.Fatalf("SocketPath(local-a): %v", err)
	}
	socketB, err := headless.SocketPath(cfgDir, "local-b")
	if err != nil {
		t.Fatalf("SocketPath(local-b): %v", err)
	}
	lnA, err := net.Listen("unix", socketA)
	if err != nil {
		t.Fatalf("Listen(local-a): %v", err)
	}
	defer func() {
		_ = lnA.Close()
	}()
	lnB, err := net.Listen("unix", socketB)
	if err != nil {
		t.Fatalf("Listen(local-b): %v", err)
	}
	defer func() {
		_ = lnB.Close()
	}()
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions["local-a"] = headless.SessionRecord{
			SessionID:  "local-a",
			PID:        -1,
			SocketPath: socketA,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		state.Sessions["local-b"] = headless.SessionRecord{
			SessionID:  "local-b",
			PID:        -1,
			SocketPath: socketB,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"detach", "all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Sessions) != 0 {
		t.Fatalf("expected all sessions removed, got %d", len(state.Sessions))
	}
	if _, err := os.Stat(socketA); !os.IsNotExist(err) {
		t.Fatalf("socket A still exists")
	}
	if _, err := os.Stat(socketB); !os.IsNotExist(err) {
		t.Fatalf("socket B still exists")
	}
}

func TestDetachAllNoSessions(t *testing.T) {
	testutil.SetLingonConfigEnv(t)
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"detach", "all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestConfigDirForLoaderUsesLingonConfigDirEnv(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv(lingon.ConfigDirEnv, cfgDir)

	loader := lingon.NewLoader()
	_ = NewRootCommand(loader)

	if got := configDirForLoader(loader); got != cfgDir {
		t.Fatalf("configDirForLoader() = %q, want %q", got, cfgDir)
	}
}

func TestDetachMultipleSessionIDs(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	if err := os.MkdirAll(headless.BaseDir(cfgDir), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	socketA, err := headless.SocketPath(cfgDir, "local-a")
	if err != nil {
		t.Fatalf("SocketPath(local-a): %v", err)
	}
	socketB, err := headless.SocketPath(cfgDir, "local-b")
	if err != nil {
		t.Fatalf("SocketPath(local-b): %v", err)
	}
	socketC, err := headless.SocketPath(cfgDir, "local-c")
	if err != nil {
		t.Fatalf("SocketPath(local-c): %v", err)
	}
	lnA, err := net.Listen("unix", socketA)
	if err != nil {
		t.Fatalf("Listen(local-a): %v", err)
	}
	defer func() {
		_ = lnA.Close()
	}()
	lnB, err := net.Listen("unix", socketB)
	if err != nil {
		t.Fatalf("Listen(local-b): %v", err)
	}
	defer func() {
		_ = lnB.Close()
	}()
	lnC, err := net.Listen("unix", socketC)
	if err != nil {
		t.Fatalf("Listen(local-c): %v", err)
	}
	defer func() {
		_ = lnC.Close()
	}()
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions["local-a"] = headless.SessionRecord{SessionID: "local-a", PID: -1, SocketPath: socketA, StartedAt: now, LastSeenAt: now, Status: "running"}
		state.Sessions["local-b"] = headless.SessionRecord{SessionID: "local-b", PID: -1, SocketPath: socketB, StartedAt: now, LastSeenAt: now, Status: "running"}
		state.Sessions["local-c"] = headless.SessionRecord{SessionID: "local-c", PID: -1, SocketPath: socketC, StartedAt: now, LastSeenAt: now, Status: "running"}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"detach", "local-a", "local-c"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Sessions["local-a"]; ok {
		t.Fatalf("local-a still present")
	}
	if _, ok := state.Sessions["local-c"]; ok {
		t.Fatalf("local-c still present")
	}
	if _, ok := state.Sessions["local-b"]; !ok {
		t.Fatalf("local-b unexpectedly removed")
	}
}

func TestDetachRejectsAllWithSessionIDs(t *testing.T) {
	testutil.SetLingonConfigEnv(t)
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"detach", "all", "local-a"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when combining all with session ids")
	}
}

func TestDetachCompletionListsSessionIDs(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	if err := os.MkdirAll(headless.BaseDir(cfgDir), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	socketA, err := headless.SocketPath(cfgDir, "local-a")
	if err != nil {
		t.Fatalf("SocketPath(local-a): %v", err)
	}
	socketB, err := headless.SocketPath(cfgDir, "local-b")
	if err != nil {
		t.Fatalf("SocketPath(local-b): %v", err)
	}
	lnA, err := net.Listen("unix", socketA)
	if err != nil {
		t.Fatalf("Listen(local-a): %v", err)
	}
	defer func() {
		_ = lnA.Close()
	}()
	lnB, err := net.Listen("unix", socketB)
	if err != nil {
		t.Fatalf("Listen(local-b): %v", err)
	}
	defer func() {
		_ = lnB.Close()
	}()
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions["local-a"] = headless.SessionRecord{SessionID: "local-a", PID: -1, SocketPath: socketA, StartedAt: now, LastSeenAt: now, Status: "running"}
		state.Sessions["local-b"] = headless.SessionRecord{SessionID: "local-b", PID: -1, SocketPath: socketB, StartedAt: now, LastSeenAt: now, Status: "running"}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewDetachCommand(loader)
	if cmd.ValidArgsFunction == nil {
		t.Fatalf("detach command missing ValidArgsFunction")
	}

	all, directive := cmd.ValidArgsFunction(cmd, nil, "local-")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if len(all) != 2 || all[0] != "local-a" || all[1] != "local-b" {
		t.Fatalf("all suggestions = %v, want [local-a local-b]", all)
	}

	rest, _ := cmd.ValidArgsFunction(cmd, []string{"local-a"}, "local-")
	if len(rest) != 1 || rest[0] != "local-b" {
		t.Fatalf("remaining suggestions = %v, want [local-b]", rest)
	}

	allMode, _ := cmd.ValidArgsFunction(cmd, nil, "all")
	if len(allMode) != 1 || allMode[0] != "all" {
		t.Fatalf("all suggestions = %v, want [all]", allMode)
	}

	none, _ := cmd.ValidArgsFunction(cmd, []string{"all"}, "local-")
	if len(none) != 0 {
		t.Fatalf("expected no suggestions after all, got %v", none)
	}
}

func TestSendHeadlessRejectsUnknownSessionIDBeforeFallback(t *testing.T) {
	cfgDir := testutil.SetLingonConfigEnv(t)
	socketPath := listenTestHeadlessSocket(t, cfgDir, "local-a")
	store := headless.NewStore(cfgDir)
	if err := store.WithLock(func(state *headless.State) error {
		now := time.Now().UTC()
		state.Sessions["local-a"] = headless.SessionRecord{
			SessionID:  "local-a",
			PID:        os.Getpid(),
			SocketPath: socketPath,
			StartedAt:  now,
			LastSeenAt: now,
			Status:     "running",
		}
		return nil
	}); err != nil {
		t.Fatalf("store.WithLock: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{"send", "--headless", "local-typo", "--", "echo"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected unknown session id error")
	}
	if !strings.Contains(err.Error(), `headless session "local-typo" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func listenTestHeadlessSocket(t *testing.T, cfgDir, sessionID string) string {
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
