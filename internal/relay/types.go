package relay

import "time"

// User represents a relay user account.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	TOTPSecret   string    `json:"totp_secret"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session represents a terminal session (future multi-session support).
type Session struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Name         string    `json:"name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	Status       string    `json:"status"`
}

// ActiveSession tracks the current host connection and controller lease.
type ActiveSession struct {
	SessionID          string    `json:"session_id"`
	HostConnectionID   string    `json:"host_connection_id"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	ControllerClientID string    `json:"controller_client_id"`
	Cols               int       `json:"cols"`
	Rows               int       `json:"rows"`
}

// ShareScope defines share token access levels.
type ShareScope string

// Share scope values for share tokens.
const (
	ShareScopeView    ShareScope = "view"
	ShareScopeControl ShareScope = "control"
)

// ShareToken grants access to a session.
type ShareToken struct {
	Token     string     `json:"token"`
	SessionID string     `json:"session_id"`
	Scope     ShareScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// AccessToken represents a short-lived access token.
type AccessToken struct {
	Token      string    `json:"token"`
	Username   string    `json:"username"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// RefreshToken represents a long-lived refresh token.
type RefreshToken struct {
	Token      string     `json:"token"`
	Username   string     `json:"username"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// IsExpired returns true when the token is expired or revoked.
func (t ShareToken) IsExpired(now time.Time) bool {
	if t.RevokedAt != nil {
		return true
	}
	if t.ExpiresAt == nil {
		return false
	}
	return now.After(*t.ExpiresAt)
}

// AllowsControl returns true if the token permits control.
func (t ShareToken) AllowsControl() bool {
	return t.Scope == ShareScopeControl
}
