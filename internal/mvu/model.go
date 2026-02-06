package mvu

import "time"

// Model is the MVU in-memory model for one composed frame.
type Model struct {
	State   State
	Cursor  Cursor
	Now     time.Time
	Resolve ResolveOptions
}

// NewModel constructs an MVU model snapshot.
func NewModel(state State, cursor Cursor, now time.Time, resolve ResolveOptions) Model {
	return Model{
		State:   state,
		Cursor:  cursor,
		Now:     now,
		Resolve: resolve,
	}
}

// ResolveState resolves model overlay visibility.
func (m Model) ResolveState() Resolved {
	return Resolve(m.State, m.Cursor, m.Now, m.Resolve)
}

// Compose renders the resolved model over a base frame.
func (m Model) Compose(base []byte, cols, rows int) []byte {
	resolved := m.ResolveState()
	return ComposeResolved(base, cols, rows, m.Cursor, resolved)
}

// ComposeTopOverlay renders only top-row overlays for the model.
func (m Model) ComposeTopOverlay(cols int) []byte {
	resolved := m.ResolveState()
	if !resolved.TopOverlayVisible {
		return nil
	}
	return ComposeTopOverlayResolved(cols, m.Cursor, resolved)
}

// WithState returns a model copy with updated state.
func (m Model) WithState(state State) Model {
	m.State = state
	return m
}

// WithCursor returns a model copy with updated cursor.
func (m Model) WithCursor(cursor Cursor) Model {
	m.Cursor = cursor
	return m
}

// WithNow returns a model copy with updated timestamp.
func (m Model) WithNow(now time.Time) Model {
	m.Now = now
	return m
}

// WithResolve returns a model copy with updated resolve options.
func (m Model) WithResolve(resolve ResolveOptions) Model {
	m.Resolve = resolve
	return m
}
