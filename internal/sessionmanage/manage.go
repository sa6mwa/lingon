package sessionmanage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Request targets a session on its owning machine. IDs must be unique per operation.
type Request struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
}

// Result acknowledges readiness or a committed display-name change.
type Result struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Error     string `json:"error,omitempty"`
}

// Validate rejects malformed operations and terminal control characters in names.
func Validate(req Request) error {
	if req.RequestID == "" || len(req.RequestID) > 128 || req.SessionID == "" || len(req.SessionID) > 256 {
		return fmt.Errorf("request_id and session_id are required and must be bounded")
	}
	if req.Kind != "create" && req.Kind != "rename" {
		return fmt.Errorf("unknown session operation")
	}
	if req.Kind == "rename" {
		return ValidateName(req.Name)
	}
	if req.Name != "" {
		return fmt.Errorf("creation uses the host's default session name")
	}
	return nil
}

// ValidateName validates a user-visible session name, independent of its socket path.
func ValidateName(name string) error {
	if name != strings.TrimSpace(name) || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
		return fmt.Errorf("session name must contain 1–80 characters without surrounding whitespace")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("session name must not contain control characters")
		}
	}
	return nil
}

type receipt struct {
	request Request
	result  Result
	done    chan struct{}
}

// Manager serializes lifecycle work and remembers the last 256 completed requests.
// Clients must never automatically retry an uncertain operation with a new ID.
type Manager struct {
	mu       sync.Mutex
	active   bool
	receipts map[string]*receipt
	order    []string
}

// New constructs a lifecycle manager.
func New() *Manager { return &Manager{receipts: make(map[string]*receipt)} }

// Execute coalesces duplicate requests without blocking terminal transport readers.
func (m *Manager) Execute(ctx context.Context, req Request, run func() Result) Result {
	if err := Validate(req); err != nil {
		return Result{Error: err.Error()}
	}
	m.mu.Lock()
	if old := m.receipts[req.RequestID]; old != nil {
		if old.request != req {
			m.mu.Unlock()
			return Result{Error: "request ID already belongs to a different operation"}
		}
		m.mu.Unlock()
		select {
		case <-old.done:
			return old.result
		case <-ctx.Done():
			return Result{Error: ctx.Err().Error()}
		}
	}
	if m.active {
		m.mu.Unlock()
		return Result{Error: "session management busy; try again when the current operation finishes"}
	}
	m.active = true
	r := &receipt{request: req, done: make(chan struct{})}
	m.receipts[req.RequestID] = r
	m.mu.Unlock()
	r.result = run()
	m.mu.Lock()
	m.active = false
	close(r.done)
	m.order = append(m.order, req.RequestID)
	if len(m.order) > 256 {
		delete(m.receipts, m.order[0])
		m.order = m.order[1:]
	}
	m.mu.Unlock()
	return r.result
}
