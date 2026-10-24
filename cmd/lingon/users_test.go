package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/testutil"
)

func TestOfflineUserAdminRevokesTokensAndShareTokens(t *testing.T) {
	stateDir := testutil.TempDir(t)
	now := time.Now().UTC()
	store := relay.NewStore()
	store.CreateSession(relay.Session{
		ID:       "alice-session",
		Username: "alice",
		Status:   "active",
	})
	store.CreateSession(relay.Session{
		ID:       "bob-session",
		Username: "bob",
		Status:   "active",
	})
	access, err := store.CreateAccessToken("alice", time.Hour, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	refresh, err := store.CreateRefreshToken("alice", time.Hour, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	aliceShare, err := store.CreateShareToken("alice-session", relay.ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken alice: %v", err)
	}
	bobShare, err := store.CreateShareToken("bob-session", relay.ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken bob: %v", err)
	}
	if err := store.Save(stateDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	usersFile := filepath.Join(stateDir, "users.json")
	users := relay.NewUserStore()
	users.Upsert(relay.User{Username: "alice", PasswordHash: "previous", TOTPSecret: "previous"})
	if err := users.Save(usersFile); err != nil {
		t.Fatal(err)
	}
	admin, err := relay.NewUserAdmin(relay.UserAdminOptions{DataDir: stateDir, UsersFile: usersFile})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.MutateUser(context.Background(), relay.UserMutationRequest{Action: "chpasswd", Username: "alice", Password: "new-password"}); err != nil {
		t.Fatal(err)
	}

	loaded, err := relay.LoadStore(stateDir)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if _, ok := loaded.AccessTokens[access.Token]; ok {
		t.Fatalf("alice access token was not removed")
	}
	if _, ok := loaded.RefreshTokens[refresh.Token]; ok {
		t.Fatalf("alice refresh token was not removed")
	}
	revokedAliceShare, ok := loaded.ShareTokens[aliceShare.Token]
	if !ok {
		t.Fatalf("alice share token missing")
	}
	if revokedAliceShare.RevokedAt == nil {
		t.Fatalf("alice share token was not revoked")
	}
	untouchedBobShare, ok := loaded.ShareTokens[bobShare.Token]
	if !ok {
		t.Fatalf("bob share token missing")
	}
	if untouchedBobShare.RevokedAt != nil {
		t.Fatalf("bob share token was revoked")
	}
}
