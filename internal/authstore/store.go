package authstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

type storedState struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

var legacyStateKeys = map[string]bool{
	"endpoint":           true,
	"access_token":       true,
	"access_expires_at":  true,
	"refresh_token":      true,
	"refresh_expires_at": true,
}

// NormalizeEndpoint normalizes endpoint input for consistent auth keying.
func NormalizeEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint must include scheme")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		return "", fmt.Errorf("endpoint must include scheme")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		parsed.Scheme = strings.ToLower(parsed.Scheme)
	case "wss":
		parsed.Scheme = "https"
	case "ws":
		parsed.Scheme = "http"
	default:
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimRight(parsed.String(), "/"), nil
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
	states, err := loadStates(path)
	if err != nil {
		return State{}, err
	}
	if len(states) == 0 {
		return State{}, os.ErrNotExist
	}
	if len(states) > 1 {
		return State{}, fmt.Errorf("multiple auth entries found; load by endpoint")
	}
	for _, state := range states {
		return state, nil
	}
	return State{}, os.ErrNotExist
}

// LoadForEndpoint reads auth state for the selected endpoint from disk.
func LoadForEndpoint(path, endpoint string) (State, error) {
	states, err := loadStates(path)
	if err != nil {
		return State{}, err
	}
	normalized, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return State{}, err
	}
	state, ok := states[normalized]
	if !ok {
		return State{}, fmt.Errorf("auth for endpoint %s: %w", normalized, os.ErrNotExist)
	}
	state.Endpoint = normalized
	return state, nil
}

// Endpoints returns normalized endpoint keys stored in the auth file.
func Endpoints(path string) ([]string, error) {
	states, err := loadStates(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(states))
	for endpoint := range states {
		out = append(out, endpoint)
	}
	sort.Strings(out)
	return out, nil
}

// WithLock runs fn while holding an exclusive lock for the auth state.
func WithLock(path string, fn func() error) error {
	lock, err := lockFile(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.unlock()
	}()
	return fn()
}

// Save writes auth state to disk.
func Save(path string, state State) error {
	normalized, err := NormalizeEndpoint(state.Endpoint)
	if err != nil {
		return err
	}
	state.Endpoint = normalized
	states, err := loadStates(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if states == nil {
		states = make(map[string]State)
	}
	for key, existing := range states {
		if key == normalized {
			continue
		}
		if state.RefreshToken != "" && existing.RefreshToken == state.RefreshToken {
			delete(states, key)
			continue
		}
		if state.RefreshToken == "" && state.AccessToken != "" && existing.AccessToken == state.AccessToken {
			delete(states, key)
		}
	}
	states[normalized] = state
	return saveStates(path, states)
}

// Delete removes auth state for the selected endpoint. It is idempotent.
func Delete(path, endpoint string) error {
	normalized, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return err
	}
	states, err := loadStates(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	delete(states, normalized)
	if len(states) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return saveStates(path, states)
}

func loadStates(path string) (map[string]State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var top map[string]json.RawMessage
	if err := decoder.Decode(&top); err != nil {
		return nil, err
	}
	if len(top) == 0 {
		return map[string]State{}, nil
	}
	if isLegacyState(top) {
		var legacy State
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		if strings.TrimSpace(legacy.Endpoint) == "" {
			return nil, fmt.Errorf("legacy auth state is missing endpoint")
		}
		normalized, err := NormalizeEndpoint(legacy.Endpoint)
		if err != nil {
			return nil, err
		}
		legacy.Endpoint = normalized
		return map[string]State{normalized: legacy}, nil
	}
	states := make(map[string]State, len(top))
	for endpoint, raw := range top {
		normalized, err := NormalizeEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		if _, exists := states[normalized]; exists {
			return nil, fmt.Errorf("duplicate auth entries for endpoint %s", normalized)
		}
		var stored storedState
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, err
		}
		states[normalized] = State{
			Endpoint:         normalized,
			AccessToken:      stored.AccessToken,
			AccessExpiresAt:  stored.AccessExpiresAt,
			RefreshToken:     stored.RefreshToken,
			RefreshExpiresAt: stored.RefreshExpiresAt,
		}
	}
	return states, nil
}

func isLegacyState(top map[string]json.RawMessage) bool {
	for key := range top {
		if legacyStateKeys[key] {
			return true
		}
	}
	return false
}

func saveStates(path string, states map[string]State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	stored := make(map[string]storedState, len(keys))
	for _, key := range keys {
		normalized, err := NormalizeEndpoint(key)
		if err != nil {
			return err
		}
		state := states[key]
		stored[normalized] = storedState{
			AccessToken:      state.AccessToken,
			AccessExpiresAt:  state.AccessExpiresAt,
			RefreshToken:     state.RefreshToken,
			RefreshExpiresAt: state.RefreshExpiresAt,
		}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
