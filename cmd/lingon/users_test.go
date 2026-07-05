package main

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/testutil"
)

func TestRevokePersistedUserAuthRevokesTokensAndShareTokens(t *testing.T) {
	stateDir := testutil.TempDir(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
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

	if err := revokePersistedUserAuth(stateDir, "alice", now.Add(time.Minute)); err != nil {
		t.Fatalf("revokePersistedUserAuth: %v", err)
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
