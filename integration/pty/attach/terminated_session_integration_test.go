//go:build integration
// +build integration

package integrationptyattach_test

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
		SessionID:       secondID,
		Cols:            120,
		Rows:            30,
		RefreshInterval: 100 * time.Millisecond,
	})

	attach.Send("echo ATTACH_TERMINATED_TAB_BAR_READY\n")
	if !screenContainsWithin(attach, "ATTACH_TERMINATED_TAB_BAR_READY", 2*time.Second) {
		t.Fatalf("expected active attach view to move below top row before asserting tabs")
	}
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

	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if !strings.Contains(row, "alpha-2") {
			return fmt.Errorf("expected alpha-2 tab to remain during grace, got %q", row)
		}
		return nil
	})
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Not connected") {
			return fmt.Errorf("unexpected not connected overlay after termination")
		}
		return nil
	})
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

type sessionRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Headless bool   `json:"headless"`
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
	var rows []sessionRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}
