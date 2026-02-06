package attach_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRemovesTerminatedSession(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-1",
		SessionName: "alpha",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	host.SendCtrlL()
	host.Send("c")

	secondID, err := waitForSessionName(t, h.Clock(), h.Endpoint(), h.AccessToken(), "alpha-2", 5*time.Second)
	if err != nil {
		t.Fatalf("wait for second session: %v", err)
	}

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: secondID,
		Cols:      120,
		Rows:      30,
	})

	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if !strings.Contains(row, "alpha") || !strings.Contains(row, "alpha-2") {
			return fmt.Errorf("expected tabs for alpha and alpha-2, got %q", row)
		}
		return nil
	})

	host.SendCtrlL()
	host.Send("Q")

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if strings.Contains(row, "alpha-2") {
			return fmt.Errorf("expected alpha-2 tab removed, got %q", row)
		}
		return nil
	})
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Not connected") {
			return fmt.Errorf("unexpected not connected overlay after termination")
		}
		return nil
	})

	waitForClientCount(t, h, "host-1", 1, 3*time.Second)

	host.SendCtrlL()
	host.Send("Q")

	if err := waitForNoSessions(h.Clock(), h.Endpoint(), h.AccessToken(), 5*time.Second); err != nil {
		t.Fatalf("expected no sessions: %v", err)
	}

	if ok, err := attach.WaitErr(5 * time.Second); !ok {
		t.Fatalf("attach did not exit after final session closed")
	} else if err != nil && err != context.Canceled && !strings.Contains(err.Error(), "no sessions available") {
		t.Fatalf("attach exit error: %v", err)
	}
}

func waitForSessionName(t *testing.T, clk clock.Clock, endpoint, token, name string, timeout time.Duration) (string, error) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		sessions, err := fetchSessions(endpoint, token)
		if err == nil {
			for _, session := range sessions {
				if session.Name == name {
					return session.ID, nil
				}
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return "", fmt.Errorf("session %q not found", name)
}

func waitForNoSessions(clk clock.Clock, endpoint, token string, timeout time.Duration) error {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		sessions, err := fetchSessions(endpoint, token)
		if err == nil && len(sessions) == 0 {
			return nil
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return fmt.Errorf("sessions still present")
}

type sessionRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func fetchSessions(endpoint, token string) ([]sessionRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // test harness TLS
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
	var rows []sessionRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}
