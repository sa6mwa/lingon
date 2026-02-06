package relay

import (
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultAccessTokenTTL is the default access token TTL.
	DefaultAccessTokenTTL = 15 * time.Minute
	// DefaultRefreshTokenTTL is the default refresh token TTL.
	DefaultRefreshTokenTTL = 365 * 24 * time.Hour
)

var (
	// ErrTokenNotFound is returned when a token is missing.
	ErrTokenNotFound = errors.New("token not found")
	// ErrTokenExpired is returned when a token is expired.
	ErrTokenExpired = errors.New("token expired")
	// ErrTokenRevoked is returned when a token is revoked.
	ErrTokenRevoked = errors.New("token revoked")
)

// IsExpired reports whether the access token is expired.
func (t AccessToken) IsExpired(now time.Time) bool {
	return now.After(t.ExpiresAt)
}

// IsExpired reports whether the refresh token is expired or revoked.
func (t RefreshToken) IsExpired(now time.Time) bool {
	if t.RevokedAt != nil {
		return true
	}
	return now.After(t.ExpiresAt)
}

// CreateAccessToken generates and stores an access token.
func (s *Store) CreateAccessToken(username string, ttl time.Duration, now time.Time) (AccessToken, error) {
	return s.CreateAccessTokenForRefresh(username, "", "", ttl, now)
}

// CreateAccessTokenForRefresh generates an access token linked to a refresh token.
func (s *Store) CreateAccessTokenForRefresh(username, refreshToken, clientType string, ttl time.Duration, now time.Time) (AccessToken, error) {
	if s == nil {
		return AccessToken{}, fmt.Errorf("store is nil")
	}
	if username == "" {
		return AccessToken{}, fmt.Errorf("username is required")
	}
	value, err := randomToken(defaultTokenBytes)
	if err != nil {
		return AccessToken{}, err
	}
	token := AccessToken{
		Token:        value,
		Username:     username,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
		LastUsedAt:   now,
		RefreshToken: refreshToken,
		ClientType:   clientType,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AccessTokens == nil {
		s.AccessTokens = make(map[string]AccessToken)
	}
	s.AccessTokens[token.Token] = token
	return token, nil
}

// CreateRefreshToken generates and stores a refresh token.
func (s *Store) CreateRefreshToken(username string, ttl time.Duration, now time.Time) (RefreshToken, error) {
	return s.CreateRefreshTokenForClient(username, "", ttl, now)
}

// CreateRefreshTokenForClient generates a refresh token tagged with client type.
func (s *Store) CreateRefreshTokenForClient(username, clientType string, ttl time.Duration, now time.Time) (RefreshToken, error) {
	if s == nil {
		return RefreshToken{}, fmt.Errorf("store is nil")
	}
	if username == "" {
		return RefreshToken{}, fmt.Errorf("username is required")
	}
	value, err := randomToken(defaultTokenBytes)
	if err != nil {
		return RefreshToken{}, err
	}
	token := RefreshToken{
		Token:      value,
		Username:   username,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		ClientType: clientType,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.RefreshTokens == nil {
		s.RefreshTokens = make(map[string]RefreshToken)
	}
	s.RefreshTokens[token.Token] = token
	return token, nil
}

// ValidateAccessToken checks access token validity.
func (s *Store) ValidateAccessToken(value string, now time.Time) (AccessToken, error) {
	if s == nil {
		return AccessToken{}, fmt.Errorf("store is nil")
	}
	if value == "" {
		return AccessToken{}, ErrTokenNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.AccessTokens[value]
	if !ok {
		return AccessToken{}, ErrTokenNotFound
	}
	if token.IsExpired(now) {
		return AccessToken{}, ErrTokenExpired
	}
	token.LastUsedAt = now
	s.AccessTokens[value] = token
	return token, nil
}

// ValidateRefreshToken checks refresh token validity.
func (s *Store) ValidateRefreshToken(value string, now time.Time) (RefreshToken, error) {
	if s == nil {
		return RefreshToken{}, fmt.Errorf("store is nil")
	}
	if value == "" {
		return RefreshToken{}, ErrTokenNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.RefreshTokens[value]
	if !ok {
		return RefreshToken{}, ErrTokenNotFound
	}
	if token.IsExpired(now) {
		return RefreshToken{}, ErrTokenExpired
	}
	if token.RevokedAt != nil {
		return RefreshToken{}, ErrTokenRevoked
	}
	token.LastUsedAt = &now
	s.RefreshTokens[value] = token
	return token, nil
}

// RevokeRefreshToken revokes a refresh token.
func (s *Store) RevokeRefreshToken(value string, now time.Time) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.RefreshTokens[value]
	if !ok {
		return ErrTokenNotFound
	}
	token.RevokedAt = &now
	s.RefreshTokens[value] = token
	return nil
}

// RevokeRefreshTokenAndAccess revokes a refresh token and removes linked access tokens.
func (s *Store) RevokeRefreshTokenAndAccess(value string, now time.Time) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.RefreshTokens[value]
	if !ok {
		return ErrTokenNotFound
	}
	token.RevokedAt = &now
	s.RefreshTokens[value] = token
	for accessValue, access := range s.AccessTokens {
		if access.RefreshToken == value {
			delete(s.AccessTokens, accessValue)
		}
	}
	return nil
}

// DeleteAccessToken removes an access token by value.
func (s *Store) DeleteAccessToken(value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.AccessTokens, value)
}

// MarkTokenClientType updates access and refresh tokens with a client type.
func (s *Store) MarkTokenClientType(accessValue, clientType string) {
	if s == nil {
		return
	}
	if clientType == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	access, ok := s.AccessTokens[accessValue]
	if !ok {
		return
	}
	if access.ClientType == "host" && clientType != "host" {
		return
	}
	access.ClientType = clientType
	s.AccessTokens[accessValue] = access
	if access.RefreshToken == "" {
		return
	}
	refresh, ok := s.RefreshTokens[access.RefreshToken]
	if !ok {
		return
	}
	if refresh.ClientType == "host" && clientType != "host" {
		return
	}
	refresh.ClientType = clientType
	s.RefreshTokens[access.RefreshToken] = refresh
}

// RevokeClientTokens revokes non-host tokens for a user and returns the count revoked.
func (s *Store) RevokeClientTokens(username string, now time.Time) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	revoked := 0
	for tokenValue, refresh := range s.RefreshTokens {
		if refresh.Username != username {
			continue
		}
		if refresh.ClientType == "host" {
			continue
		}
		refresh.RevokedAt = &now
		s.RefreshTokens[tokenValue] = refresh
		revoked++
	}
	for accessValue, access := range s.AccessTokens {
		if access.Username != username {
			continue
		}
		if access.ClientType == "host" {
			continue
		}
		delete(s.AccessTokens, accessValue)
	}
	return revoked
}
