package headless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateVersion = 1

// SessionRecord is persisted metadata for a headless session.
type SessionRecord struct {
	SessionID  string    `json:"session_id"`
	PID        int       `json:"pid"`
	SocketPath string    `json:"socket_path"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Endpoint   string    `json:"endpoint,omitempty"`
	Offline    bool      `json:"offline"`
	Status     string    `json:"status,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

// WatcherRecord is persisted metadata for a local state watcher endpoint.
type WatcherRecord struct {
	ID         string    `json:"id"`
	PID        int       `json:"pid"`
	SocketPath string    `json:"socket_path"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// State is the versioned persisted headless session state.
type State struct {
	Version  int                      `json:"version"`
	Sessions map[string]SessionRecord `json:"sessions"`
	Watchers map[string]WatcherRecord `json:"watchers,omitempty"`
}

// Store persists headless state atomically.
type Store struct {
	path string
}

// NewStore constructs a Store from config dir.
func NewStore(configDir string) *Store {
	return &Store{path: StatePath(configDir)}
}

// Path returns the persisted state file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// WithLock executes fn under an exclusive lock and persists changes when fn returns nil.
func (s *Store) WithLock(fn func(state *State) error) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if fn == nil {
		return fmt.Errorf("update function is required")
	}
	lock, err := lockFile(s.path)
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.unlock()
	}()

	state, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	beforeJSON, err := marshalState(state)
	if err != nil {
		return err
	}
	beforeSignal, err := marshalSignificantState(state)
	if err != nil {
		return err
	}
	pruneWatchers(state)
	if err := fn(state); err != nil {
		return err
	}
	state = normalizeState(state)
	afterJSON, err := marshalState(state)
	if err != nil {
		return err
	}
	changed := !bytes.Equal(beforeJSON, afterJSON)
	if changed {
		if err := writeFileAtomic(s.path, afterJSON, 0o600); err != nil {
			return err
		}
	}
	afterSignal, err := marshalSignificantState(state)
	if err != nil {
		return err
	}
	if changed && !bytes.Equal(beforeSignal, afterSignal) {
		notifyStateWatchers(state)
	}
	return nil
}

// Load returns the persisted state. Missing files return an empty initialized state.
func (s *Store) Load() (*State, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	lock, err := lockFile(s.path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.unlock()
	}()
	return s.loadUnlocked()
}

func (s *Store) loadUnlocked() (*State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out State
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return normalizeState(&out), nil
}

func emptyState() *State {
	return &State{
		Version:  stateVersion,
		Sessions: map[string]SessionRecord{},
		Watchers: map[string]WatcherRecord{},
	}
}

func normalizeState(state *State) *State {
	if state == nil {
		return emptyState()
	}
	if state.Version == 0 {
		state.Version = stateVersion
	}
	if state.Sessions == nil {
		state.Sessions = map[string]SessionRecord{}
	}
	if state.Watchers == nil {
		state.Watchers = map[string]WatcherRecord{}
	}
	return state
}

func marshalState(state *State) ([]byte, error) {
	return json.MarshalIndent(normalizeState(state), "", "  ")
}

type stateSignal struct {
	Sessions map[string]sessionSignal `json:"sessions"`
}

type sessionSignal struct {
	SessionID  string    `json:"session_id"`
	PID        int       `json:"pid"`
	SocketPath string    `json:"socket_path"`
	StartedAt  time.Time `json:"started_at"`
	Endpoint   string    `json:"endpoint,omitempty"`
	Offline    bool      `json:"offline"`
	Status     string    `json:"status,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

func marshalSignificantState(state *State) ([]byte, error) {
	state = normalizeState(state)
	signal := stateSignal{
		Sessions: make(map[string]sessionSignal, len(state.Sessions)),
	}
	for id, rec := range state.Sessions {
		signal.Sessions[id] = sessionSignal{
			SessionID:  rec.SessionID,
			PID:        rec.PID,
			SocketPath: rec.SocketPath,
			StartedAt:  rec.StartedAt,
			Endpoint:   rec.Endpoint,
			Offline:    rec.Offline,
			Status:     rec.Status,
			LastError:  rec.LastError,
		}
	}
	return json.Marshal(signal)
}

func pruneWatchers(state *State) {
	state = normalizeState(state)
	for id, rec := range state.Watchers {
		if rec.PID > 0 && !PIDAlive(rec.PID) {
			delete(state.Watchers, id)
			continue
		}
		if !SocketExists(rec.SocketPath) {
			delete(state.Watchers, id)
		}
	}
}

func notifyStateWatchers(state *State) {
	state = normalizeState(state)
	for _, watcher := range state.Watchers {
		if watcher.SocketPath == "" {
			continue
		}
		_ = notifyWatcherSocket(watcher.SocketPath)
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
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
