package lingon

import (
	"context"
	"fmt"
	"strings"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/headless"
)

func headlessSessionSource(configDir string) func(context.Context) ([]attach.SessionInfo, error) {
	return func(context.Context) ([]attach.SessionInfo, error) {
		store := headless.NewStore(configDir)
		records, err := store.Reconcile()
		if err != nil {
			return nil, err
		}
		out := make([]attach.SessionInfo, 0, len(records))
		for _, rec := range records {
			name := strings.TrimSpace(rec.SessionID)
			out = append(out, attach.SessionInfo{
				ID:           rec.SessionID,
				Name:         name,
				Status:       rec.Status,
				Offline:      rec.Offline,
				LastActiveAt: rec.StartedAt,
			})
		}
		return out, nil
	}
}

func headlessSocketResolver(configDir string) func(sessionID string) (string, error) {
	return func(sessionID string) (string, error) {
		normalized, err := headless.NormalizeSessionID(sessionID)
		if err != nil {
			return "", err
		}
		store := headless.NewStore(configDir)
		records, err := store.Reconcile()
		if err != nil {
			return "", err
		}
		for _, rec := range records {
			if rec.SessionID != normalized {
				continue
			}
			socketPath := strings.TrimSpace(rec.SocketPath)
			if socketPath == "" {
				return "", fmt.Errorf("headless session %q has no socket path", normalized)
			}
			return socketPath, nil
		}
		return "", fmt.Errorf("headless session %q not found", normalized)
	}
}
