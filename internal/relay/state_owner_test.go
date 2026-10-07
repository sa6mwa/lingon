//go:build !windows

package relay

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func newTestStateOwner(t *testing.T) (*StateOwner, *HTTPServer, string) {
	t.Helper()
	dir := testutil.TempDir(t)
	owner, err := NewStateOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	store, err := owner.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "users.json")
	users, err := owner.LoadUsers(path)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(store, users, nil, nil, nil)
	t.Cleanup(func() { server.Close("test cleanup") })
	server.DataDir, server.UsersFile = dir, path
	return owner, server, path
}

func TestUserAdminWorksWithLongDataDirectoryPaths(t *testing.T) {
	for _, relative := range []bool{false, true} {
		name := "absolute"
		if relative {
			name = "relative"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(testutil.TempDir(t), strings.Repeat("deep-", 12), strings.Repeat("nested-", 10), strings.Repeat("state-", 12))
			owner, err := NewStateOwner(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			store, err := owner.LoadStore()
			if err != nil {
				t.Fatal(err)
			}
			usersPath := filepath.Join(dir, "users.json")
			users, err := owner.LoadUsers(usersPath)
			if err != nil {
				t.Fatal(err)
			}
			server := NewHTTPServer(store, users, nil, nil, nil)
			server.DataDir, server.UsersFile = dir, usersPath
			t.Cleanup(func() { server.Close("test cleanup") })
			if err := owner.ListenUsers(usersPath, server); err != nil {
				t.Fatalf("valid long data directory cannot listen: %v", err)
			}
			socket := userAdminSocket(dir)
			info, err := os.Lstat(socket)
			if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
				t.Fatalf("socket did not retain its private data-directory location: %v", err)
			}
			clientDir := dir
			if relative {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				clientDir, err = filepath.Rel(cwd, dir)
				if err != nil {
					t.Fatal(err)
				}
			}
			admin, err := NewUserAdmin(UserAdminOptions{DataDir: clientDir, UsersFile: usersPath})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = admin.Close() })
			created, err := admin.MutateUser(context.Background(), UserMutationRequest{Action: "add", Username: "alice", Password: "test-only"})
			if err != nil || created.Username != "alice" {
				t.Fatalf("long-path live administration failed: %v", err)
			}
			if _, ok := server.Users.Get("alice"); !ok {
				t.Fatal("mutation bypassed the live owner")
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(socket); !os.IsNotExist(err) {
				t.Fatalf("closed owner left its socket behind: %v", err)
			}
		})
	}
}

func TestStateOwnerTransfersLeaseAndFencesOldWriters(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	if _, err := NewStateOwner(owner.dir); !errors.Is(err, ErrStateOwned) {
		t.Fatalf("second owner: %v", err)
	}
	server.Users.Upsert(User{Username: "alice"})
	if err := server.Users.Save(usersPath); err != nil {
		t.Fatal(err)
	}
	if err := server.Store.Save(owner.dir); err != nil {
		t.Fatal(err)
	}
	if err := server.Store.Save(testutil.TempDir(t)); err == nil {
		t.Fatal("owned store wrote to another directory")
	}
	var joined sync.WaitGroup
	for i := 0; i < 8; i++ {
		joined.Add(1)
		go func() {
			defer joined.Done()
			if err := owner.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		}()
	}
	joined.Wait()
	if _, err := owner.LoadStore(); !errors.Is(err, ErrStateOwnerClosed) {
		t.Fatalf("closed load: %v", err)
	}
	next, err := NewStateOwner(owner.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = next.Close() }()
	current, err := next.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	current.CreateSession(Session{ID: "new-owner-session", Username: "bob"})
	if err := current.Save(owner.dir); err != nil {
		t.Fatal(err)
	}
	if err := server.Store.Save(owner.dir); !errors.Is(err, ErrStateOwnerClosed) {
		t.Fatalf("late state write: %v", err)
	}
	if err := server.Users.Save(usersPath); !errors.Is(err, ErrStateOwnerClosed) {
		t.Fatalf("late users write: %v", err)
	}
	loaded, err := LoadStore(owner.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.GetSession("new-owner-session"); !ok {
		t.Fatal("old owner replaced new owner state")
	}
}

func TestUserAdminCannotFallBackWhenOwnerIsUnavailable(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	server.Users.Upsert(User{Username: "alice", PasswordHash: "unchanged"})
	if err := server.Users.Save(usersPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: usersPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	_, err = admin.MutateUser(context.Background(), UserMutationRequest{Action: "chpasswd", Username: "alice"})
	if err == nil || !strings.Contains(err.Error(), "no offline fallback") {
		t.Fatalf("unavailable owner: %v", err)
	}
	after, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unavailable owner fell back to user-file writes")
	}
	if _, err := os.Stat(filepath.Join(owner.dir, storeFilename)); !os.IsNotExist(err) {
		t.Fatal("unavailable owner created a relay snapshot")
	}
}

func TestPrivateUserAdministrationPermissionsAndErrorIdentity(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	if err := owner.ListenUsers(usersPath, server); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(owner.dir, "relay-state.lock"): 0o600,
		filepath.Dir(userAdminSocket(owner.dir)):     0o700,
		userAdminSocket(owner.dir):                   0o600,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private control permissions for %s: %v", filepath.Base(path), err)
		}
	}
	admin, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: usersPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	created, err := admin.MutateUser(context.Background(), UserMutationRequest{Action: "add", Username: "alice", Password: "known-password"})
	if err != nil || created.Username != "alice" || created.Password != "known-password" || created.TOTPSecret == "" {
		t.Fatalf("add response missing required CLI values: %v", err)
	}
	for _, tc := range []struct {
		req  UserMutationRequest
		want error
	}{
		{UserMutationRequest{Action: "add", Username: "alice"}, ErrUserExists},
		{UserMutationRequest{Action: "delete", Username: "missing"}, ErrUserNotFound},
		{UserMutationRequest{Action: "add"}, ErrUsernameRequired},
		{UserMutationRequest{Action: "add", Username: "bad/name"}, ErrUsernameInvalid},
	} {
		if _, err := admin.MutateUser(context.Background(), tc.req); !errors.Is(err, tc.want) {
			t.Fatalf("administrative error identity: %v", err)
		}
	}
	wrong, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: filepath.Join(owner.dir, "other-users.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wrong.Close() }()
	if _, err := wrong.MutateUser(context.Background(), UserMutationRequest{Action: "delete", Username: "alice"}); err == nil {
		t.Fatal("wrong users file was routed silently")
	}
	if _, ok := server.Users.Get("alice"); !ok {
		t.Fatal("mismatched configuration deleted a live user")
	}
}

type userMutatorFunc func(context.Context, UserMutationRequest) (UserMutationResult, error)

func (f userMutatorFunc) MutateUser(ctx context.Context, req UserMutationRequest) (UserMutationResult, error) {
	return f(ctx, req)
}

func TestStateOwnerCloseCancelsAndJoinsAdministrativeRequest(t *testing.T) {
	owner, _, usersPath := newTestStateOwner(t)
	started := make(chan struct{})
	mutator := userMutatorFunc(func(ctx context.Context, _ UserMutationRequest) (UserMutationResult, error) {
		close(started)
		<-ctx.Done()
		return UserMutationResult{}, ctx.Err()
	})
	if err := owner.ListenUsers(usersPath, mutator); err != nil {
		t.Fatal(err)
	}
	admin, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: usersPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	result := make(chan error, 1)
	go func() {
		_, err := admin.MutateUser(context.Background(), UserMutationRequest{Action: "add", Username: "alice"})
		result <- err
	}()
	<-started
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("cancelled mutation returned success")
	}
	next, err := NewStateOwner(owner.dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
}

func TestStateOwnerReclaimsStaleSocketAndRejectsForeignPaths(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	path := userAdminSocket(owner.dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()
	if err := owner.ListenUsers(usersPath, server); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewStateOwner(owner.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = next.Close() }()
	if err := os.WriteFile(path, []byte("preserve this unrelated file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := next.ListenUsers(usersPath, server); err == nil {
		t.Fatal("administration overwrote a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "preserve this unrelated file" {
		t.Fatal("regular control-path file was changed")
	}
}

func TestStateOwnerRejectsSymlinkedControlPaths(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	foreign := testutil.TempDir(t)
	if err := os.Symlink(foreign, filepath.Join(owner.dir, ".admin")); err != nil {
		t.Fatal(err)
	}
	if err := owner.ListenUsers(usersPath, server); err == nil {
		t.Fatal("administration followed a foreign directory symlink")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil || len(entries) != 0 {
		t.Fatal("foreign directory was modified")
	}
}

func TestUserAdministrationRecognizesDirectoryAliasesAndNewParents(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-parent", true: "new-parent"}[nested], func(t *testing.T) {
			owner, server, usersPath := newTestStateOwner(t)
			if nested {
				usersPath = filepath.Join(owner.dir, "nested", "users.json")
				server.UsersFile = usersPath
			}
			if err := owner.ListenUsers(usersPath, server); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(testutil.TempDir(t), "state-alias")
			if err := os.Symlink(owner.dir, alias); err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(owner.dir, usersPath)
			if err != nil {
				t.Fatal(err)
			}
			admin, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: filepath.Join(alias, relative)})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = admin.Close() }()
			for _, req := range []UserMutationRequest{
				{Action: "add", Username: "alice", Password: "first-password"},
				{Action: "chpasswd", Username: "alice", Password: "second-password"},
			} {
				if _, err := admin.MutateUser(context.Background(), req); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok := server.Users.Get("alice"); !ok {
				t.Fatal("aliased users-file mutation missed live owner")
			}
		})
	}
}

// This helper has null inherited stdio and only two explicit test-owned pipes.
func TestStateOwnerProcessHelper(t *testing.T) {
	if os.Getenv("LINGON_TEST_STATE_OWNER_PROCESS") != "1" {
		return
	}
	dir := os.Getenv("LINGON_TEST_STATE_OWNER_DIRECTORY")
	ready := os.NewFile(3, "owned-state-ready")
	defer ready.Close()
	hold := os.NewFile(4, "owned-state-hold")
	defer hold.Close()
	owner, err := NewStateOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	mutator := userMutatorFunc(func(context.Context, UserMutationRequest) (UserMutationResult, error) {
		return UserMutationResult{}, nil
	})
	if err := owner.ListenUsers(filepath.Join(dir, "users.json"), mutator); err != nil {
		t.Fatal(err)
	}
	if _, err := ready.WriteString("READY\n"); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, hold)
	_ = owner.Close()
}

func TestStateOwnerRecoversAfterOwnedProcessCrash(t *testing.T) {
	dir := testutil.TempDir(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	holdR, holdW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []*os.File{readyR, readyW, holdR, holdW} {
		t.Cleanup(func() { _ = file.Close() })
	}
	cmd := exec.Command(executable, "-test.run=^TestStateOwnerProcessHelper$")
	cmd.Env = append(os.Environ(), "LINGON_TEST_STATE_OWNER_PROCESS=1", "LINGON_TEST_STATE_OWNER_DIRECTORY="+dir)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.ExtraFiles = []*os.File{readyW, holdR}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	_ = readyW.Close()
	_ = holdR.Close()
	line, err := bufio.NewReader(readyR).ReadString('\n')
	if err != nil || line != "READY\n" {
		t.Fatalf("owned child readiness: %v", err)
	}
	if _, err := NewStateOwner(dir); !errors.Is(err, ErrStateOwned) {
		t.Fatalf("cross-process contention: %v", err)
	}
	// Crash only this noninteractive test child, never the runner/process group.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("crashed child exited successfully")
	}
	owner, err := NewStateOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	if err := owner.ListenUsers(filepath.Join(dir, "users.json"), userMutatorFunc(func(context.Context, UserMutationRequest) (UserMutationResult, error) {
		return UserMutationResult{}, nil
	})); err != nil {
		t.Fatal(err)
	}
}

func TestUserMutationFailureDoesNotPublishCredentialsOrAcknowledgeSuccess(t *testing.T) {
	owner, server, usersPath := newTestStateOwner(t)
	server.Users.Upsert(User{Username: "alice", PasswordHash: "unchanged-password-hash"})
	if err := server.Users.Save(usersPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	// Make persistence fail at its final rename without changing caller state.
	if err := os.Mkdir(filepath.Join(owner.dir, storeFilename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := owner.ListenUsers(usersPath, server); err != nil {
		t.Fatal(err)
	}
	admin, err := NewUserAdmin(UserAdminOptions{DataDir: owner.dir, UsersFile: usersPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	result, err := admin.MutateUser(context.Background(), UserMutationRequest{Action: "chpasswd", Username: "alice", Password: "private-new-password"})
	if err == nil || result.Password != "" || strings.Contains(err.Error(), "private-new-password") {
		t.Fatal("failed mutation exposed or acknowledged new credentials")
	}
	after, readErr := os.ReadFile(usersPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("new credentials persisted before revocations")
	}
}
