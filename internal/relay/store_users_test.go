package relay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/testutil"
)

func TestReloadUsersFromDisk(t *testing.T) {
	dir := testutil.TempDir(t)
	now := time.Now().UTC()
	path := filepath.Join(dir, "users.json")

	store := NewUserStore()
	user := User{
		Username:     "alice",
		PasswordHash: "old",
		TOTPSecret:   "oldsecret",
		CreatedAt:    now,
	}
	store.Upsert(user)
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	updated := NewUserStore()
	updated.Upsert(User{
		Username:     user.Username,
		PasswordHash: "new",
		TOTPSecret:   "newsecret",
		CreatedAt:    user.CreatedAt,
	})
	if err := updated.Save(path); err != nil {
		t.Fatalf("Save updated: %v", err)
	}
	if err := store.ReloadFromDisk(path); err != nil {
		t.Fatalf("ReloadFromDisk: %v", err)
	}
	reloaded, ok := store.Get("alice")
	if !ok {
		t.Fatalf("expected user after reload")
	}
	if reloaded.PasswordHash != "new" || reloaded.TOTPSecret != "newsecret" {
		t.Fatalf("user fields not updated")
	}

	empty := NewUserStore()
	if err := empty.Save(path); err != nil {
		t.Fatalf("Save empty: %v", err)
	}
	if err := store.ReloadFromDisk(path); err != nil {
		t.Fatalf("ReloadFromDisk: %v", err)
	}
	if _, ok := store.Get("alice"); ok {
		t.Fatalf("user should be removed")
	}
}

func TestUserReloadLoopDetectsContentChangeSameMtime(t *testing.T) {
	dir := testutil.TempDir(t)
	now := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "users.json")

	store := NewUserStore()
	user := User{
		Username:     "alice",
		PasswordHash: "old",
		TOTPSecret:   "oldsecret",
		CreatedAt:    now,
	}
	store.Upsert(user)
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	clk := clock.NewMock()
	changed := make(chan struct{}, 1)
	reloader, err := NewUserReloader(context.Background(), UserReloadOptions{Path: path, Store: store, Clock: clk, OnReload: func([]string) { changed <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reloader.Close() }()

	updated := NewUserStore()
	updated.Upsert(User{
		Username:     user.Username,
		PasswordHash: "new",
		TOTPSecret:   "newsecret",
		CreatedAt:    user.CreatedAt,
	})
	if err := updated.Save(path); err != nil {
		t.Fatalf("Save updated: %v", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("Chtimes updated: %v", err)
	}

	clk.Add(userReloadInterval - time.Nanosecond)
	select {
	case <-changed:
		t.Fatal("reload occurred before its clock deadline")
	default:
	}
	clk.Add(time.Nanosecond)
	<-changed
	loaded, ok := store.Get("alice")
	if !ok || loaded.TOTPSecret != "newsecret" {
		t.Fatal("expected reload with updated totp secret")
	}

}

func TestCredentialChangedUsersDetectsCredentialsAndRemoval(t *testing.T) {
	changed := credentialChangedUsers(
		[]User{
			{Username: "alice", PasswordHash: "old", TOTPSecret: "same"},
			{Username: "bob", PasswordHash: "same", TOTPSecret: "old"},
			{Username: "carol", PasswordHash: "removed", TOTPSecret: "removed"},
			{Username: "dave", PasswordHash: "same", TOTPSecret: "same"},
		},
		[]User{
			{Username: "alice", PasswordHash: "new", TOTPSecret: "same"},
			{Username: "bob", PasswordHash: "same", TOTPSecret: "new"},
			{Username: "dave", PasswordHash: "same", TOTPSecret: "same"},
			{Username: "erin", PasswordHash: "added", TOTPSecret: "added"},
		},
	)
	want := []string{"alice", "bob", "carol"}
	if len(changed) != len(want) {
		t.Fatalf("changed users = %v, want %v", changed, want)
	}
	for i := range want {
		if changed[i] != want[i] {
			t.Fatalf("changed users = %v, want %v", changed, want)
		}
	}
}

func TestUserReloaderReconcilesChangesBeforeRegistration(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "users.json")
	current := NewUserStore()
	current.Upsert(User{Username: "alice", PasswordHash: "previous"})
	disk := NewUserStore()
	disk.Upsert(User{Username: "alice", PasswordHash: "changed-before-registration"})
	if err := disk.Save(path); err != nil {
		t.Fatal(err)
	}
	var changed []string
	r, err := NewUserReloader(context.Background(), UserReloadOptions{Path: path, Store: current, Clock: clock.NewMock(), OnReload: func(users []string) { changed = users }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if len(changed) != 1 || changed[0] != "alice" {
		t.Fatal("registration missed changed credentials")
	}
	user, ok := current.Get("alice")
	if !ok || user.PasswordHash != "changed-before-registration" {
		t.Fatal("registration did not reconcile latest file")
	}
}

func TestUserReloadLoopInvalidatesChangedCredentials(t *testing.T) {
	dir := testutil.TempDir(t)
	now := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "users.json")

	users := NewUserStore()
	users.Upsert(User{
		Username:     "alice",
		PasswordHash: "old",
		TOTPSecret:   "oldsecret",
		CreatedAt:    now,
	})
	if err := users.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	state := NewStore()
	access, err := state.CreateAccessToken("alice", DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	refresh, err := state.CreateRefreshToken("alice", DefaultRefreshTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	state.CreateSession(Session{
		ID:           "alice-session",
		Username:     "alice",
		CreatedAt:    now,
		LastActiveAt: now,
		Status:       "active",
	})
	share, err := state.CreateShareToken("alice-session", ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	server := NewHTTPServer(state, users, NewAuthenticator(users), nil, NewHub(nil))
	server.DataDir = dir

	clk := clock.NewMock()
	changed := make(chan struct{}, 1)
	reloader, err := NewUserReloader(context.Background(), UserReloadOptions{Path: path, Store: users, Clock: clk, OnReload: func(changedUsers []string) {
		server.InvalidateReloadedUsers(changedUsers, now)
		changed <- struct{}{}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reloader.Close() }()

	updated := NewUserStore()
	updated.Upsert(User{
		Username:     "alice",
		PasswordHash: "new",
		TOTPSecret:   "oldsecret",
		CreatedAt:    now,
	})
	if err := updated.Save(path); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	clk.Add(userReloadInterval)
	<-changed
	_, accessErr := state.ValidateAccessToken(access.Token, now)
	_, refreshErr := state.ValidateRefreshToken(refresh.Token, now)
	storedShare, shareOK := state.GetShareToken(share.Token)
	if !errors.Is(accessErr, ErrTokenNotFound) || !errors.Is(refreshErr, ErrTokenNotFound) || !shareOK || storedShare.RevokedAt == nil {
		t.Fatal("expected reload to revoke access, refresh, and share token")
	}

}
