package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		return nil, fmt.Errorf("load store %s: %w", path, err)
	}
	store, err := LoadStoreFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("load store %s: %w", path, err)
	}
	return store, nil
}

// LoadStoreFromBytes unmarshals store data and ensures maps are initialized.
func LoadStoreFromBytes(data []byte) (*Store, error) {
	var s Store
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&s); err != nil {
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
	return writeFileAtomic(path, data, 0o600)
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

// GetSession returns the session by ID, if present.
func (s *Store) GetSession(sessionID string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.Sessions[sessionID]
	return session, ok
}

// ListSessions returns sessions for a user.
func (s *Store) ListSessions(username string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessions := make([]Session, 0)
	for _, session := range s.Sessions {
		if session.Username == username {
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].LastActiveAt.Equal(sessions[j].LastActiveAt) {
			return sessions[i].LastActiveAt.After(sessions[j].LastActiveAt)
		}
		if !sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions
}

// ListActiveSessions returns active sessions for a user.
func (s *Store) ListActiveSessions(username string) []Session {
	sessions := s.ListSessions(username)
	if len(sessions) == 0 {
		return sessions
	}
	active := sessions[:0]
	for _, session := range sessions {
		if session.Status == "active" {
			active = append(active, session)
		}
	}
	return active
}

// MarkSessionInactive marks a session inactive if the host connection matches.
func (s *Store) MarkSessionInactive(sessionID, hostConnID string, now time.Time) bool {
	if sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active := s.Active[sessionID]
	if active.HostConnectionID != "" && hostConnID != "" && active.HostConnectionID != hostConnID {
		return false
	}
	session, ok := s.Sessions[sessionID]
	if !ok {
		return false
	}
	changed := false
	if session.Status != "inactive" {
		session.Status = "inactive"
		changed = true
	}
	if !now.IsZero() {
		session.LastActiveAt = now
		changed = true
	}
	s.Sessions[sessionID] = session
	delete(s.Active, sessionID)
	return changed
}

// PruneSessions removes inactive sessions older than maxAge.
func (s *Store) PruneSessions(now time.Time, maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	cutoff := now.Add(-maxAge)
	pruned := 0

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.Sessions {
		if session.Status == "active" {
			continue
		}
		if session.LastActiveAt.IsZero() || session.LastActiveAt.After(cutoff) {
			continue
		}
		delete(s.Sessions, id)
		delete(s.Active, id)
		for token, share := range s.ShareTokens {
			if share.SessionID == id {
				delete(s.ShareTokens, token)
			}
		}
		pruned++
	}
	return pruned
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

// RevokeShareTokensForUsername revokes all share tokens for a user's sessions.
func (s *Store) RevokeShareTokensForUsername(username string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionIDs := make(map[string]struct{})
	for _, session := range s.Sessions {
		if session.Username == username {
			sessionIDs[session.ID] = struct{}{}
		}
	}
	revoked := 0
	for token, share := range s.ShareTokens {
		if _, ok := sessionIDs[share.SessionID]; !ok {
			continue
		}
		if share.RevokedAt != nil {
			continue
		}
		share.RevokedAt = &now
		s.ShareTokens[token] = share
		revoked++
	}
	return revoked
}

// GetShareToken returns a share token.
func (s *Store) GetShareToken(token string) (ShareToken, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored, ok := s.ShareTokens[token]
	return stored, ok
}

// SessionOwner returns the username for a session, if it exists.
func (s *Store) SessionOwner(sessionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.Sessions[sessionID]
	if !ok {
		return "", false
	}
	return session.Username, true
}

// ShareTokenOwner returns the username owning the token's session, if available.
func (s *Store) ShareTokenOwner(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	share, ok := s.ShareTokens[token]
	if !ok {
		return "", false
	}
	session, ok := s.Sessions[share.SessionID]
	if !ok {
		return "", false
	}
	return session.Username, true
}

// ListShareTokens returns share tokens for a user.
func (s *Store) ListShareTokens(username string) []ShareToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionIDs := make(map[string]struct{})
	for _, session := range s.Sessions {
		if session.Username == username {
			sessionIDs[session.ID] = struct{}{}
		}
	}
	tokens := make([]ShareToken, 0)
	for _, token := range s.ShareTokens {
		if _, ok := sessionIDs[token.SessionID]; !ok {
			continue
		}
		tokens = append(tokens, token)
	}
	sort.Slice(tokens, func(i, j int) bool {
		if !tokens[i].CreatedAt.Equal(tokens[j].CreatedAt) {
			return tokens[i].CreatedAt.After(tokens[j].CreatedAt)
		}
		if tokens[i].SessionID != tokens[j].SessionID {
			return tokens[i].SessionID < tokens[j].SessionID
		}
		return tokens[i].Token < tokens[j].Token
	})
	return tokens
}

// ListShareTokensFiltered returns share tokens for a user filtered by status.
func (s *Store) ListShareTokensFiltered(username string, statuses map[ShareTokenStatus]bool, now time.Time) []ShareToken {
	tokens := s.ListShareTokens(username)
	if len(statuses) == 0 {
		statuses = map[ShareTokenStatus]bool{ShareTokenStatusValid: true}
	}
	if statuses[ShareTokenStatusValid] && statuses[ShareTokenStatusRevoked] && statuses[ShareTokenStatusExpired] {
		return tokens
	}
	filtered := make([]ShareToken, 0, len(tokens))
	for _, token := range tokens {
		valid, revoked, expired := shareTokenStatus(token, now)
		if (valid && statuses[ShareTokenStatusValid]) ||
			(revoked && statuses[ShareTokenStatusRevoked]) ||
			(expired && statuses[ShareTokenStatusExpired]) {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func shareTokenStatus(token ShareToken, now time.Time) (valid, revoked, expired bool) {
	isRevoked := token.RevokedAt != nil
	isExpired := token.ExpiresAt != nil && now.After(*token.ExpiresAt)
	valid = !isRevoked && !isExpired
	revoked = isRevoked && !isExpired
	expired = isExpired && !isRevoked
	return valid, revoked, expired
}
