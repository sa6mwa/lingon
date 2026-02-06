package attach

import "time"

// SessionInfo describes a remote session summary.
type SessionInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	Status       string    `json:"status"`
	LastActiveAt time.Time `json:"last_active_at"`
}

// SessionTabID returns the stable session identifier used for tab selection.
func (s SessionInfo) SessionTabID() string {
	return s.ID
}

// SessionTabName returns the user-facing tab label.
func (s SessionInfo) SessionTabName() string {
	return s.Name
}

// SessionTabLastActiveAt returns the session's last activity timestamp.
func (s SessionInfo) SessionTabLastActiveAt() time.Time {
	return s.LastActiveAt
}
