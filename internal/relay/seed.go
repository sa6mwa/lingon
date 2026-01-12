package relay

import "time"

const defaultSessionID = "session_test"

// SeedDefaultSession ensures a default session exists for the user.
func SeedDefaultSession(store *Store, username string) {
	if store == nil {
		return
	}
	store.mu.RLock()
	_, exists := store.Sessions[defaultSessionID]
	store.mu.RUnlock()
	if exists {
		return
	}

	store.CreateSession(Session{
		ID:           defaultSessionID,
		Username:     username,
		CreatedAt:    time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
		Status:       "active",
	})
	store.SetActiveSession(ActiveSession{
		SessionID:  defaultSessionID,
		Cols:       80,
		Rows:       24,
		LastSeenAt: time.Now().UTC(),
	})
}
