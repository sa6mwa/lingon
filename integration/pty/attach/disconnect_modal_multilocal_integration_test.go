//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/ptytest"
)

func TestDisconnectModalCountdownStableAfterLocalPtyTabSwitch(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "host-local", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-local"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}

	host.SendCtrlL()
	host.Send("c")
	_ = waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-local",
		Cols:      80,
		Rows:      24,
		BackoffPolicy: backoff.Policy{
			Base:   3 * time.Second,
			Factor: 1,
			Max:    3 * time.Second,
		},
		OnActive: func(sessionID string) {
			activeMu.Lock()
			activeID = sessionID
			activeMu.Unlock()
		},
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	primeTabsByCount(t, attachSess, 2)
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)

	h.StopServer()
	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Wait(200 * time.Millisecond)

	re := regexp.MustCompile(`reconnecting in (\d+)s`)
	values := collectCountdownSamples(t, attachSess, re, 1200*time.Millisecond)
	if hasInterleavedZero(values) {
		t.Fatalf("countdown flickered to 0s after local pty tab switch: %v", values)
	}
}
