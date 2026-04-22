//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

func waitForSessionCountSession(t *testing.T, clk clock.Clock, endpoint, token, authPath string, want int, timeout time.Duration) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if authPath != "" {
			if refreshed, err := relayclient.EnsureAccessToken(context.Background(), endpoint, authPath); err == nil && refreshed.AccessToken != "" {
				token = refreshed.AccessToken
			}
		}
		found, err := fetchSessionIDsSession(endpoint, token)
		if err == nil && len(found) == want {
			return
		}
		advanceTestClock(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d sessions", want)
}

type activeState struct {
	mu      sync.Mutex
	id      string
	viewsMu sync.Mutex
	views   map[string]*attach.Client
}

func newActiveState() *activeState {
	return &activeState{views: make(map[string]*attach.Client)}
}

func (s *activeState) onActive(id string) {
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
}

func (s *activeState) onView(id string, client *attach.Client) {
	s.viewsMu.Lock()
	s.views[id] = client
	s.viewsMu.Unlock()
}

func waitForActiveOrAnyReadySession(t *testing.T, clk clock.Clock, state *activeState, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		state.mu.Lock()
		current := state.id
		state.mu.Unlock()
		if current != "" && (prev == "" || current != prev) {
			state.viewsMu.Lock()
			client := state.views[current]
			state.viewsMu.Unlock()
			if client != nil && client.Connected() {
				return current
			}
		}
		state.viewsMu.Lock()
		for id, client := range state.views {
			if prev != "" && id == prev {
				continue
			}
			if client != nil && client.Connected() {
				state.viewsMu.Unlock()
				return id
			}
		}
		state.viewsMu.Unlock()
		advanceTestClock(clk, 50*time.Millisecond)
	}
	if prev == "" {
		t.Fatalf("timed out waiting for active or ready session")
	}
	t.Fatalf("timed out waiting for active or ready session after %q", prev)
	return ""
}

func tryWaitForActiveSessionReadySession(clk clock.Clock, state *activeState, prev string, timeout time.Duration) (string, bool) {
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		state.mu.Lock()
		current := state.id
		state.mu.Unlock()
		if current != "" && current != prev {
			state.viewsMu.Lock()
			client := state.views[current]
			state.viewsMu.Unlock()
			if client != nil && client.Connected() {
				return current, true
			}
		}
		advanceTestClock(clk, 50*time.Millisecond)
	}
	return "", false
}

func advanceActiveTabSession(t *testing.T, sess *ptytest.PTYSession, cmd string, clk clock.Clock, state *activeState, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		sess.SendCtrlL()
		sess.Send(cmd)
		advanceTestClock(sess.Clock(), 150*time.Millisecond)
		if next, ok := tryWaitForActiveSessionReadySession(clk, state, prev, 400*time.Millisecond); ok {
			return next
		}
	}
	t.Fatalf("timed out waiting for active session ready after %q", prev)
	return ""
}

type advancingClock interface {
	clock.Clock
	Add(time.Duration)
}

func advanceTestClock(clk clock.Clock, d time.Duration) {
	ptytest.Advance(clk, d)
	if clk == nil || d <= 0 {
		return
	}
	if _, ok := clk.(advancingClock); ok {
		extra := d / 4
		if extra < 10*time.Millisecond {
			extra = 10 * time.Millisecond
		}
		if extra > 50*time.Millisecond {
			extra = 50 * time.Millisecond
		}
		time.Sleep(extra)
	}
}

func eventuallyWithClock(t *testing.T, clk clock.Clock, timeout, step time.Duration, check func() error) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if err := check(); err == nil {
			return
		}
		advanceTestClock(clk, step)
	}
	if err := check(); err != nil {
		t.Fatalf("%v", err)
	}
}

func fetchSessionIDsSession(endpoint, token string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   time.Second,
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

func primeTabsByCountSession(t *testing.T, sess *ptytest.PTYSession, count int) {
	t.Helper()
	for i := 0; i < count-1; i++ {
		sess.SendCtrlL()
		sess.Send("n")
		advanceTestClock(sess.Clock(), 150*time.Millisecond)
	}
	advanceTestClock(sess.Clock(), 300*time.Millisecond)
}
