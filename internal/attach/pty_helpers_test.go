package attach_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func waitForSessions(t *testing.T, clk clock.Clock, endpoint, token string, ids []string) {
	t.Helper()
	waitForSessionsWithTimeout(t, clk, endpoint, token, ids, 3*time.Second)
}

func waitForSessionsWithTimeout(t *testing.T, clk clock.Clock, endpoint, token string, ids []string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	var lastErr error
	for {
		if ptytest.Now(clk).After(deadline) {
			if lastErr != nil {
				t.Fatalf("timed out waiting for sessions %v (last error: %v)", ids, lastErr)
			}
			t.Fatalf("timed out waiting for sessions %v", ids)
		}
		found, err := fetchSessionIDs(endpoint, token)
		if err == nil {
			all := true
			for _, id := range ids {
				if !found[id] {
					all = false
					break
				}
			}
			if all {
				return
			}
		} else {
			lastErr = err
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func fetchSessionIDs(endpoint, token string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // test harness TLS
			DisableKeepAlives: true,
		},
		Timeout: time.Second,
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sessions status: %s", resp.Status)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(rows))
	for _, row := range rows {
		found[row.ID] = true
	}
	return found, nil
}

func waitForClientCount(t *testing.T, h *ptytest.Harness, sessionID string, wantMin int, timeout time.Duration) {
	t.Helper()
	clk := h.Clock()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			t.Fatalf("timed out waiting for %d clients on %s (have %d)", wantMin, sessionID, h.ClientCount(sessionID))
		}
		if h.ClientCount(sessionID) >= wantMin {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func waitForHost(t *testing.T, h *ptytest.Harness, sessionID string, timeout time.Duration) {
	t.Helper()
	clk := h.Clock()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			t.Fatalf("timed out waiting for host on %s", sessionID)
		}
		if h.HasHost(sessionID) {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func waitForSessionSeq(t *testing.T, h *ptytest.Harness, sessionID string, minSeq uint64, timeout time.Duration) {
	t.Helper()
	clk := h.Clock()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			t.Fatalf("timed out waiting for seq >= %d on %s (have %d)", minSeq, sessionID, h.SessionSeq(sessionID))
		}
		if h.SessionSeq(sessionID) >= minSeq {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func waitForFramePayload(t *testing.T, clk clock.Clock, rec *ptytest.WSRecorder, role, sessionID string, dir ptytest.Direction, payload string, min int, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			t.Fatalf("timed out waiting for %s frames (%s %s) on %s", payload, role, dir, sessionID)
		}
		count := 0
		for _, rec := range rec.Frames() {
			if role != "" && rec.Role != role {
				continue
			}
			if sessionID != "" && rec.SessionID != sessionID {
				continue
			}
			if dir != "" && rec.Direction != dir {
				continue
			}
			if payload != "" && rec.Payload != payload {
				continue
			}
			count++
		}
		if count >= min {
			return
		}
		ptytest.Advance(clk, 20*time.Millisecond)
	}
}
