package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
		return sessions[i].ID < sessions[j].ID
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
	return headless.DetachSession(context.Background(), configDir, sessionID)
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
