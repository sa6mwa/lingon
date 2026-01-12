package relay

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"time"
)

const defaultTokenBytes = 32

// CreateShareToken generates and stores a share token.
func (s *Store) CreateShareToken(sessionID string, scope ShareScope, ttl time.Duration, now time.Time) (ShareToken, error) {
	if s == nil {
		return ShareToken{}, fmt.Errorf("store is nil")
	}
	if scope != ShareScopeView && scope != ShareScopeControl {
		return ShareToken{}, fmt.Errorf("invalid scope")
	}
	if sessionID == "" {
		return ShareToken{}, fmt.Errorf("session id is required")
	}
	value, err := randomToken(defaultTokenBytes)
	if err != nil {
		return ShareToken{}, err
	}
	var expires *time.Time
	if ttl > 0 {
		exp := now.Add(ttl)
		expires = &exp
	}
	share := ShareToken{
		Token:     value,
		SessionID: sessionID,
		Scope:     scope,
		CreatedAt: now,
		ExpiresAt: expires,
	}
	s.AddShareToken(share)
	return share, nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(buf), nil
}
