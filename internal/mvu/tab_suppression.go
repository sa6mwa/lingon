package mvu

import "sync"

// SessionTabSuppression tracks host tab suppression keyed by session ID.
type SessionTabSuppression struct {
	mu        sync.Mutex
	sessionID string
}

// Set updates suppression for a session.
func (s *SessionTabSuppression) Set(sessionID string, on bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if on {
		s.sessionID = sessionID
	} else if s.sessionID == sessionID || sessionID == "" {
		s.sessionID = ""
	}
	s.mu.Unlock()
}

// Active reports whether suppression is active for sessionID.
func (s *SessionTabSuppression) Active(sessionID string) bool {
	if s == nil || sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID == sessionID
}

// CursorTabSuppression hides tabs until cursor reaches top row and then leaves.
type CursorTabSuppression struct {
	mu     sync.Mutex
	active bool
	sawTop bool
}

// Start enables suppression.
func (s *CursorTabSuppression) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = true
	s.sawTop = false
	s.mu.Unlock()
}

// Stop disables suppression.
func (s *CursorTabSuppression) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = false
	s.sawTop = false
	s.mu.Unlock()
}

// Resolve applies cursor row transition and returns whether suppression remains active.
func (s *CursorTabSuppression) Resolve(cursorRow int) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return false
	}
	if cursorRow <= 1 {
		s.sawTop = true
		return true
	}
	if s.sawTop && cursorRow > 1 {
		s.active = false
		s.sawTop = false
		return false
	}
	return true
}
