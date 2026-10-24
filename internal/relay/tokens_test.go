package relay

import (
	"errors"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
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

func TestMarkTokenClientTypeSkipsUnchangedMetadata(t *testing.T) {
	store := NewStore()
	access, err := store.CreateAccessToken("user", time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !store.MarkTokenClientType(access.Token, "host") {
		t.Fatal("first host classification should change token metadata")
	}
	if store.MarkTokenClientType(access.Token, "host") {
		t.Fatal("reconnect should not rewrite unchanged token metadata")
	}
	if store.MarkTokenClientType(access.Token, "client") {
		t.Fatal("client use must not downgrade a host token")
	}
}

func TestStoreSavePrunesExpiredAccessTokens(t *testing.T) {
	mock := clock.NewMock()
	store := NewStore()
	refresh, err := store.CreateRefreshToken("user", time.Hour, mock.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := store.CreateAccessTokenForRefresh("user", refresh.Token, "host", time.Minute, mock.Now())
	if err != nil {
		t.Fatal(err)
	}
	mock.Add(2 * time.Minute)
	current, err := store.CreateAccessTokenForRefresh("user", refresh.Token, "host", time.Minute, mock.Now())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := store.saveAt(dir, mock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateAccessToken(expired.Token, mock.Now()); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expired access token remained in memory: %v", err)
	}
	loaded, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.ValidateAccessToken(expired.Token, mock.Now()); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expired access token validation = %v, want not found", err)
	}
	if _, err := loaded.ValidateAccessToken(current.Token, mock.Now()); err != nil {
		t.Fatalf("current access token was lost: %v", err)
	}
	if _, err := loaded.ValidateRefreshToken(refresh.Token, mock.Now()); err != nil {
		t.Fatalf("refresh token was lost: %v", err)
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
