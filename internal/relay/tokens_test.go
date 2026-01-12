package relay

import (
	"testing"
	"time"
)

func TestShareTokenLifecycle(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	share, err := store.CreateShareToken("session", ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	if share.Token == "" {
		t.Fatalf("expected token")
	}
	if share.IsExpired(now) {
		t.Fatalf("token should not be expired")
	}
	if err := store.RevokeShareToken(share.Token, now); err != nil {
		t.Fatalf("RevokeShareToken: %v", err)
	}
	stored, ok := store.GetShareToken(share.Token)
	if !ok {
		t.Fatalf("expected token")
	}
	if !stored.IsExpired(now) {
		t.Fatalf("revoked token should be expired")
	}
}

func TestShareTokenExpires(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	share, err := store.CreateShareToken("session", ShareScopeView, time.Second, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	future := now.Add(2 * time.Second)
	if !share.IsExpired(future) {
		t.Fatalf("token should be expired")
	}
}

func TestAccessTokenLifecycle(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	access, err := store.CreateAccessToken("user", time.Minute, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	if access.Token == "" {
		t.Fatalf("expected token")
	}
	validated, err := store.ValidateAccessToken(access.Token, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if validated.Username != "user" {
		t.Fatalf("Username = %q, want %q", validated.Username, "user")
	}
	if _, err := store.ValidateAccessToken(access.Token, now.Add(2*time.Minute)); err == nil {
		t.Fatalf("expected expiry error")
	}
}

func TestRefreshTokenLifecycle(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	refresh, err := store.CreateRefreshToken("user", time.Minute, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	if refresh.Token == "" {
		t.Fatalf("expected token")
	}
	if _, err := store.ValidateRefreshToken(refresh.Token, now.Add(30*time.Second)); err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if err := store.RevokeRefreshToken(refresh.Token, now.Add(40*time.Second)); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	if _, err := store.ValidateRefreshToken(refresh.Token, now.Add(50*time.Second)); err == nil {
		t.Fatalf("expected revoked error")
	}
}
