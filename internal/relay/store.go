package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeFilename = "state.json"

// Store persists relay data to disk.
type Store struct {
	mu sync.RWMutex

	Sessions      map[string]Session       `json:"sessions"`
	Active        map[string]ActiveSession `json:"active"`
	ShareTokens   map[string]ShareToken    `json:"share_tokens"`
	AccessTokens  map[string]AccessToken   `json:"access_tokens"`
	RefreshTokens map[string]RefreshToken  `json:"refresh_tokens"`
}

// NewStore returns an initialized store.
func NewStore() *Store {
	return &Store{
		Sessions:      make(map[string]Session),
		Active:        make(map[string]ActiveSession),
		ShareTokens:   make(map[string]ShareToken),
		AccessTokens:  make(map[string]AccessToken),
		RefreshTokens: make(map[string]RefreshToken),
	}
}

// LoadStore reads persisted state if present.
func LoadStore(dir string) (*Store, error) {
	path := filepath.Join(dir, storeFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewStore(), nil
		}
		return nil, err
	}
	return LoadStoreFromBytes(data)
}

// LoadStoreFromBytes unmarshals store data and ensures maps are initialized.
func LoadStoreFromBytes(data []byte) (*Store, error) {
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]Session)
	}
	if s.Active == nil {
		s.Active = make(map[string]ActiveSession)
	}
	if s.ShareTokens == nil {
		s.ShareTokens = make(map[string]ShareToken)
	}
	if s.AccessTokens == nil {
		s.AccessTokens = make(map[string]AccessToken)
	}
	if s.RefreshTokens == nil {
		s.RefreshTokens = make(map[string]RefreshToken)
	}
	return &s, nil
}

// Save writes the store to disk.
func (s *Store) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, storeFilename)

	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RevokeTokensForUsername removes access and refresh tokens for a user.
func (s *Store) RevokeTokensForUsername(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, access := range s.AccessTokens {
		if access.Username == username {
			delete(s.AccessTokens, token)
		}
	}
	for token, refresh := range s.RefreshTokens {
		if refresh.Username == username {
			delete(s.RefreshTokens, token)
		}
	}
}

// CreateSession registers a new session.
func (s *Store) CreateSession(session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions[session.ID] = session
}

// SetActiveSession updates the active session info.
func (s *Store) SetActiveSession(active ActiveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Active[active.SessionID] = active
}

// ListSessions returns sessions for a user.
func (s *Store) ListSessions(username string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sessions []Session
	for _, session := range s.Sessions {
		if session.Username == username {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// AddShareToken registers a share token.
func (s *Store) AddShareToken(token ShareToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ShareTokens[token.Token] = token
}

// RevokeShareToken revokes a share token.
func (s *Store) RevokeShareToken(token string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.ShareTokens[token]
	if !ok {
		return fmt.Errorf("share token not found")
	}
	stored.RevokedAt = &now
	s.ShareTokens[token] = stored
	return nil
}

// GetShareToken returns a share token.
func (s *Store) GetShareToken(token string) (ShareToken, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored, ok := s.ShareTokens[token]
	return stored, ok
}
