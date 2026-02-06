package authstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/testutil"
)

func TestSaveLoadStateForEndpoint(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	state := State{
		Endpoint:         "https://LOCALHOST:12843/v1/",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}

	if err := Save(path, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadForEndpoint(path, "https://localhost:12843/v1")
	if err != nil {
		t.Fatalf("LoadForEndpoint: %v", err)
	}
	if loaded.Endpoint != "https://localhost:12843/v1" {
		t.Fatalf("Endpoint = %q, want %q", loaded.Endpoint, "https://localhost:12843/v1")
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

func TestSaveStoresMultipleEndpoints(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	first := State{
		Endpoint:         "https://one.example.com/v1",
		AccessToken:      "access-1",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh-1",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	second := State{
		Endpoint:         "https://two.example.com/v1/",
		AccessToken:      "access-2",
		AccessExpiresAt:  now.Add(15 * time.Minute),
		RefreshToken:     "refresh-2",
		RefreshExpiresAt: now.Add(36 * time.Hour),
	}

	if err := Save(path, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := Save(path, second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	one, err := LoadForEndpoint(path, "https://one.example.com/v1/")
	if err != nil {
		t.Fatalf("LoadForEndpoint one: %v", err)
	}
	if one.AccessToken != first.AccessToken {
		t.Fatalf("one.AccessToken = %q, want %q", one.AccessToken, first.AccessToken)
	}

	two, err := LoadForEndpoint(path, "https://two.example.com/v1")
	if err != nil {
		t.Fatalf("LoadForEndpoint two: %v", err)
	}
	if two.AccessToken != second.AccessToken {
		t.Fatalf("two.AccessToken = %q, want %q", two.AccessToken, second.AccessToken)
	}

	_, err = Load(path)
	if err == nil {
		t.Fatalf("Load expected error for multiple entries")
	}
}

func TestLoadForEndpointMigratesLegacyState(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	legacy := State{
		Endpoint:         "https://legacy.example.com/v1/",
		AccessToken:      "access-legacy",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "refresh-legacy",
		RefreshExpiresAt: now.Add(2 * time.Hour),
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("Marshal legacy: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile legacy: %v", err)
	}

	loaded, err := LoadForEndpoint(path, "https://legacy.example.com/v1")
	if err != nil {
		t.Fatalf("LoadForEndpoint legacy: %v", err)
	}
	if loaded.AccessToken != legacy.AccessToken {
		t.Fatalf("loaded.AccessToken = %q, want %q", loaded.AccessToken, legacy.AccessToken)
	}
}

func TestDeleteRemovesOnlySelectedEndpoint(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	first := State{
		Endpoint:         "https://one.example.com/v1",
		AccessToken:      "access-1",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh-1",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	second := State{
		Endpoint:         "https://two.example.com/v1",
		AccessToken:      "access-2",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh-2",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	if err := Save(path, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := Save(path, second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	if err := Delete(path, first.Endpoint); err != nil {
		t.Fatalf("Delete first: %v", err)
	}
	if _, err := LoadForEndpoint(path, first.Endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadForEndpoint first err = %v, want os.ErrNotExist", err)
	}
	if _, err := LoadForEndpoint(path, second.Endpoint); err != nil {
		t.Fatalf("LoadForEndpoint second: %v", err)
	}
	if err := Delete(path, first.Endpoint); err != nil {
		t.Fatalf("Delete first second time: %v", err)
	}
}

func TestSaveReplacesEntryWhenTokensMatchDifferentEndpoint(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	state := State{
		Endpoint:         "https://127.0.0.1:0/v1",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	if err := Save(path, state); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	state.Endpoint = "https://127.0.0.1:12843/v1"
	if err := Save(path, state); err != nil {
		t.Fatalf("Save updated endpoint: %v", err)
	}

	if _, err := LoadForEndpoint(path, "https://127.0.0.1:0/v1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadForEndpoint old endpoint err = %v, want os.ErrNotExist", err)
	}
	loaded, err := LoadForEndpoint(path, state.Endpoint)
	if err != nil {
		t.Fatalf("LoadForEndpoint new endpoint: %v", err)
	}
	if loaded.RefreshToken != state.RefreshToken {
		t.Fatalf("RefreshToken = %q, want %q", loaded.RefreshToken, state.RefreshToken)
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

func TestEndpointsListsNormalizedSortedKeys(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	if err := Save(path, State{
		Endpoint:         "https://two.example.com/v1/",
		AccessToken:      "a2",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "r2",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("Save second endpoint: %v", err)
	}
	if err := Save(path, State{
		Endpoint:         "https://one.example.com/v1",
		AccessToken:      "a1",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "r1",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("Save first endpoint: %v", err)
	}

	got, err := Endpoints(path)
	if err != nil {
		t.Fatalf("Endpoints: %v", err)
	}
	want := []string{
		"https://one.example.com/v1",
		"https://two.example.com/v1",
	}
	if len(got) != len(want) {
		t.Fatalf("Endpoints len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Endpoints[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEndpointsLegacyState(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()
	legacy := State{
		Endpoint:         "https://legacy.example.com/v1/",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(2 * time.Hour),
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("Marshal legacy: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile legacy: %v", err)
	}
	got, err := Endpoints(path)
	if err != nil {
		t.Fatalf("Endpoints legacy: %v", err)
	}
	if len(got) != 1 || got[0] != "https://legacy.example.com/v1" {
		t.Fatalf("Endpoints legacy = %v, want [https://legacy.example.com/v1]", got)
	}
}

func TestEndpointsMissingFile(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "missing.json")
	_, err := Endpoints(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Endpoints missing err = %v, want os.ErrNotExist", err)
	}
}

func TestNormalizeEndpointAssumesHTTPSWhenSchemeOmitted(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "host port path",
			input: "localhost:1234/v1",
			want:  "https://localhost:1234/v1",
		},
		{
			name:  "domain path",
			input: "pkt.systems/lingon/v1",
			want:  "https://pkt.systems/lingon/v1",
		},
		{
			name:  "wss translates to https",
			input: "wss://relay.example/v1",
			want:  "https://relay.example/v1",
		},
		{
			name:    "unsupported scheme",
			input:   "ftp://relay.example/v1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeEndpoint(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeEndpoint(%q): expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeEndpoint(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
