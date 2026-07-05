package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const detachWaitTimeout = 3 * time.Second

type controlDetachRequest struct {
	Reason string `json:"reason"`
}

// DetachSession force-stops a local headless session. When the daemon socket is
// reachable, it requests an explicit in-band shutdown so relay clients receive a
// clean session-closed signal before the daemon exits. If the daemon is already
// gone or unreachable, it falls back to local process cleanup.
func DetachSession(ctx context.Context, configDir, sessionID string) error {
	normalized, err := NormalizeSessionID(sessionID)
	if err != nil {
		return err
	}
	store := NewStore(configDir)
	state, err := store.Load()
	if err != nil {
		return err
	}
	rec, ok := state.Sessions[normalized]
	if !ok {
		return fmt.Errorf("headless session %q not found", normalized)
	}
	socketPath := strings.TrimSpace(rec.SocketPath)
	if socketPath == "" {
		socketPath, _ = SocketPath(configDir, normalized)
	}
	socketReachable := SocketReachable(socketPath)
	if socketReachable {
		if err := requestDetach(ctx, socketPath, "detached"); err == nil {
			return waitDetached(ctx, store, normalized, socketPath, rec.PID, rec.StartedAt)
		}
	}
	return forceDetach(ctx, store, normalized, socketPath, rec.PID, rec.StartedAt)
}

func requestDetach(ctx context.Context, socketPath, reason string) error {
	if strings.TrimSpace(socketPath) == "" {
		return fmt.Errorf("socket path is required")
	}
	payload, err := json.Marshal(controlDetachRequest{Reason: strings.TrimSpace(reason)})
	if err != nil {
		return err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   750 * time.Millisecond,
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/internal/headless/detach", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("internal detach failed: %s", resp.Status)
	}
	return nil
}

func waitDetached(ctx context.Context, store *Store, sessionID, socketPath string, pid int, startedAt time.Time) error {
	selfPID := os.Getpid()
	deadline := time.Now().Add(detachWaitTimeout)
	for {
		state, err := store.Load()
		if err != nil {
			return err
		}
		_, sessionPresent := state.Sessions[sessionID]
		socketPresent := strings.TrimSpace(socketPath) != "" && SocketExists(socketPath)
		pidAlive := pid > 0 && pid != selfPID && PIDAlive(pid)
		if !sessionPresent && !socketPresent && !pidAlive {
			return nil
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		if time.Now().After(deadline) {
			return forceDetach(ctx, store, sessionID, socketPath, pid, startedAt)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func forceDetach(ctx context.Context, store *Store, sessionID, socketPath string, pid int, startedAt time.Time) error {
	if pid > 0 && pid != os.Getpid() && PIDAlive(pid) && RecordedProcessMayMatch(pid, startedAt) {
		_ = TerminatePID(pid)
		deadline := time.Now().Add(detachWaitTimeout)
		for time.Now().Before(deadline) {
			if !PIDAlive(pid) {
				break
			}
			if ctx != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if PIDAlive(pid) {
			_ = KillPID(pid)
		}
	}
	if err := store.WithLock(func(state *State) error {
		delete(state.Sessions, sessionID)
		return nil
	}); err != nil {
		return err
	}
	store.removeOwnedSocket(socketPath)
	return nil
}
