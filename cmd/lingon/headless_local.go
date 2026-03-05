package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headless"
)

type localHeadlessSession struct {
	ID         string    `json:"id"`
	PID        int       `json:"pid"`
	SocketPath string    `json:"socket_path"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Endpoint   string    `json:"endpoint,omitempty"`
	Offline    bool      `json:"offline"`
	Status     string    `json:"status,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

func listLocalHeadlessSessions(configDir string) ([]localHeadlessSession, error) {
	store := headless.NewStore(configDir)
	records, err := store.Reconcile()
	if err != nil {
		return nil, err
	}
	out := make([]localHeadlessSession, 0, len(records))
	for _, rec := range records {
		out = append(out, localHeadlessSession{
			ID:         rec.SessionID,
			PID:        rec.PID,
			SocketPath: rec.SocketPath,
			StartedAt:  rec.StartedAt,
			LastSeenAt: rec.LastSeenAt,
			Endpoint:   rec.Endpoint,
			Offline:    rec.Offline,
			Status:     rec.Status,
			LastError:  rec.LastError,
		})
	}
	return out, nil
}

func findLocalHeadlessSession(sessions []localHeadlessSession, sessionID string) (localHeadlessSession, error) {
	normalized, err := headless.NormalizeSessionID(sessionID)
	if err != nil {
		return localHeadlessSession{}, err
	}
	for _, session := range sessions {
		if session.ID == normalized {
			return session, nil
		}
	}
	return localHeadlessSession{}, fmt.Errorf("headless session %q not found", normalized)
}

func firstLocalHeadlessSession(sessions []localHeadlessSession) (localHeadlessSession, error) {
	if len(sessions) == 0 {
		return localHeadlessSession{}, fmt.Errorf("no local headless sessions available")
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastSeenAt.Equal(sessions[j].LastSeenAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].LastSeenAt.After(sessions[j].LastSeenAt)
	})
	return sessions[0], nil
}

func localSessionsAsRelaySessions(sessions []localHeadlessSession) []lingon.Session {
	out := make([]lingon.Session, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, lingon.Session{
			ID:     session.ID,
			Name:   session.ID,
			Status: session.Status,
		})
	}
	return out
}

func detachLocalHeadlessSession(configDir, sessionID string) error {
	normalized, err := headless.NormalizeSessionID(sessionID)
	if err != nil {
		return err
	}
	store := headless.NewStore(configDir)
	var rec headless.SessionRecord
	err = store.WithLock(func(state *headless.State) error {
		found, ok := state.Sessions[normalized]
		if !ok {
			return os.ErrNotExist
		}
		rec = found
		delete(state.Sessions, normalized)
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("headless session %q not found", normalized)
		}
		return err
	}

	if rec.PID > 0 && headless.PIDAlive(rec.PID) {
		_ = headless.TerminatePID(rec.PID)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if !headless.PIDAlive(rec.PID) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if headless.PIDAlive(rec.PID) {
			_ = headless.KillPID(rec.PID)
		}
	}

	socketPath := strings.TrimSpace(rec.SocketPath)
	if socketPath == "" {
		socketPath, _ = headless.SocketPath(configDir, normalized)
	}
	if socketPath != "" {
		_ = os.Remove(socketPath)
	}
	return nil
}

func detachAllLocalHeadlessSessions(configDir string) error {
	sessions, err := listLocalHeadlessSessions(configDir)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	var errs []error
	for _, session := range sessions {
		if err := detachLocalHeadlessSession(configDir, session.ID); err != nil {
			errs = append(errs, fmt.Errorf("detach %q: %w", session.ID, err))
		}
	}
	return errors.Join(errs...)
}
