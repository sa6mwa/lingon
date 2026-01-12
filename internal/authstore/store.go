package authstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State holds persisted authentication tokens.
type State struct {
	Endpoint         string    `json:"endpoint"`
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// AccessValidAt reports whether the access token is still valid at the given time.
func (s State) AccessValidAt(t time.Time) bool {
	if s.AccessToken == "" {
		return false
	}
	if s.AccessExpiresAt.IsZero() {
		return false
	}
	return t.Before(s.AccessExpiresAt)
}

// RefreshValidAt reports whether the refresh token is still valid at the given time.
func (s State) RefreshValidAt(t time.Time) bool {
	if s.RefreshToken == "" {
		return false
	}
	if s.RefreshExpiresAt.IsZero() {
		return false
	}
	return t.Before(s.RefreshExpiresAt)
}

// Load reads auth state from disk.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

// Save writes auth state to disk.
func Save(path string, state State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
