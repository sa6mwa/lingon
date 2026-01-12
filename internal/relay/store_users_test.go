package relay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReloadUsersFromDisk(t *testing.T) {
	dir := t.TempDir()
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
	dir := t.TempDir()
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startUserReloadLoop(ctx, path, store, nil, 10*time.Millisecond); err != nil {
		t.Fatalf("startUserReloadLoop: %v", err)
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
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("Chtimes updated: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		loaded, ok := store.Get("alice")
		if ok && loaded.TOTPSecret == "newsecret" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected reload with updated totp secret")
}
