package authstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	state := State{
		Endpoint:         "https://localhost:12843/v1",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}

	if err := Save(path, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Endpoint != state.Endpoint {
		t.Fatalf("Endpoint = %q, want %q", loaded.Endpoint, state.Endpoint)
	}
	if loaded.AccessToken != state.AccessToken {
		t.Fatalf("AccessToken = %q, want %q", loaded.AccessToken, state.AccessToken)
	}
	if !loaded.AccessExpiresAt.Equal(state.AccessExpiresAt) {
		t.Fatalf("AccessExpiresAt mismatch")
	}
	if loaded.RefreshToken != state.RefreshToken {
		t.Fatalf("RefreshToken = %q, want %q", loaded.RefreshToken, state.RefreshToken)
	}
	if !loaded.RefreshExpiresAt.Equal(state.RefreshExpiresAt) {
		t.Fatalf("RefreshExpiresAt mismatch")
	}
}

func TestStateValidity(t *testing.T) {
	now := time.Now().UTC()
	state := State{
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(1 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(1 * time.Hour),
	}

	if !state.AccessValidAt(now) {
		t.Fatalf("access token should be valid")
	}
	if !state.RefreshValidAt(now) {
		t.Fatalf("refresh token should be valid")
	}
	if state.AccessValidAt(now.Add(2 * time.Minute)) {
		t.Fatalf("access token should be expired")
	}
	if state.RefreshValidAt(now.Add(2 * time.Hour)) {
		t.Fatalf("refresh token should be expired")
	}
}
