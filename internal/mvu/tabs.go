package mvu

import (
	"sort"
	"strings"

	"pkt.systems/lingon/internal/sessionorder"
)

// SessionTabSource is an input row for tab model construction.
type SessionTabSource struct {
	ID   string
	Name string
}

// BuildSessionTabsOptions controls tab rendering metadata.
type BuildSessionTabsOptions struct {
	LocalIDs map[string]bool
	Disabled map[string]bool
	Muted    map[string]bool
}

// SessionTabSourceProvider exposes tab source fields for MVU tab construction.
type SessionTabSourceProvider interface {
	SessionTabID() string
	SessionTabName() string
}

// SessionTabSources maps arbitrary session rows into MVU tab sources.
func SessionTabSources[T any](sessions []T, id func(T) string, name func(T) string) []SessionTabSource {
	if len(sessions) == 0 {
		return nil
	}
	out := make([]SessionTabSource, 0, len(sessions))
	for _, session := range sessions {
		row := SessionTabSource{}
		if id != nil {
			row.ID = id(session)
		}
		if name != nil {
			row.Name = name(session)
		}
		out = append(out, row)
	}
	return out
}

// SessionTabSourcesFrom maps provider rows into MVU tab sources.
func SessionTabSourcesFrom[T SessionTabSourceProvider](sessions []T) []SessionTabSource {
	return SessionTabSources(sessions, func(session T) string { return session.SessionTabID() }, func(session T) string { return session.SessionTabName() })
}

// SortSessionsByName sorts session rows alphanumerically by display name, then id.
func SortSessionsByName[T SessionTabSourceProvider](sessions []T) {
	sort.Slice(sessions, func(i, j int) bool {
		return sessionorder.Less(
			sessions[i].SessionTabName(),
			sessions[i].SessionTabID(),
			sessions[j].SessionTabName(),
			sessions[j].SessionTabID(),
		)
	})
}

// ShortSessionID returns a compact suffix used as a fallback session label.
func ShortSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	parts := strings.Split(id, "-")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	if len(id) > 8 {
		return id[len(id)-8:]
	}
	return id
}

// SessionLabel resolves the user-facing label for a session.
func SessionLabel(id, name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	short := ShortSessionID(id)
	if short != "" {
		return short
	}
	return id
}

// BuildSessionTabs builds tab metadata from session rows.
func BuildSessionTabs(sessions []SessionTabSource, activeID string, opts BuildSessionTabsOptions) ([]Tab, int) {
	tabs := make([]Tab, 0, len(sessions))
	activeIdx := 0
	for i, session := range sessions {
		label := SessionLabel(session.ID, session.Name)
		if opts.LocalIDs != nil && opts.LocalIDs[session.ID] && label != "" {
			label = "*" + label
		}
		disabled := false
		if opts.Disabled != nil {
			disabled = opts.Disabled[session.ID]
		}
		muted := false
		if opts.Muted != nil {
			muted = opts.Muted[session.ID]
		}
		tabs = append(tabs, Tab{
			Index:    i + 1,
			Title:    label,
			Disabled: disabled,
			Muted:    muted,
		})
		if session.ID == activeID {
			activeIdx = i
		}
	}
	return tabs, activeIdx
}

// SessionIDExists reports whether id is present in the session sources.
func SessionIDExists(sessions []SessionTabSource, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, session := range sessions {
		if session.ID == id {
			return true
		}
	}
	return false
}

// NextSessionID returns the next/previous session id using wrap-around semantics.
func NextSessionID(sessions []SessionTabSource, active string, dir int) string {
	if len(sessions) == 0 {
		return ""
	}
	idx := 0
	for i, session := range sessions {
		if session.ID == active {
			idx = i
			break
		}
	}
	found := false
	for _, session := range sessions {
		if session.ID == active {
			found = true
			break
		}
	}
	if !found {
		idx = 0
	}
	for i := 0; i < len(sessions); i++ {
		if dir > 0 {
			idx = (idx + 1) % len(sessions)
		} else {
			idx = (idx - 1 + len(sessions)) % len(sessions)
		}
		if sessions[idx].ID != active {
			break
		}
	}
	return sessions[idx].ID
}
