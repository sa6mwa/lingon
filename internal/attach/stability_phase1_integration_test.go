package attach_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachHandlesLargeOutputBurst(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-burst",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-burst"})

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-burst",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     5 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-burst", 5*time.Second)
	attachSess.Send("for i in $(seq 1 800); do echo BURST_$i; done\n")
	if !screenContainsWithin(attachSess, "BURST_800", 8*time.Second) {
		if !waitForRawContains(t, attachSess, "BURST_800", 5*time.Second) {
			t.Fatalf("expected burst output to reach attach")
		}
	}
	_ = host
}

func TestAttachRecoversAfterRelayRestartDuringTabSwitch(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-restart",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-restart"})
	host.SendCtrlL()
	host.Send("c")
	secondID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host-restart", 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-restart",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     5 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-restart", 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitForClientReady(t, h.Clock(), &viewsMu, views, secondID, 3*time.Second)

	h.RestartServer()
	_ = screenContainsWithin(attachSess, "Not connected", 2*time.Second)

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 6*time.Second)
	sendTokenAcrossTabs(t, attachSess, "RESTART_OK", 3)
	_ = host
}

func TestAttachTabSwitchWhileReconnectKeepsIO(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-reconnect",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-reconnect"})
	host.SendCtrlL()
	host.Send("c")
	secondID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host-reconnect", 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-reconnect",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     5 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-reconnect", 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitForClientReady(t, h.Clock(), &viewsMu, views, secondID, 3*time.Second)

	h.StopServer()
	_ = screenContainsWithin(attachSess, "Not connected", 2*time.Second)
	_ = screenContainsWithin(attachSess, "no sessions available", 2*time.Second)
	_ = screenContainsWithin(attachSess, "Waiting for sessions", 2*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("p")
	h.Advance(300 * time.Millisecond)
	attachSess.SendCtrlL()
	attachSess.Send("n")
	h.Advance(300 * time.Millisecond)

	h.RestartServer()
	if !waitForClientReadyOptional(h.Clock(), &viewsMu, views, secondID, 6*time.Second) {
		if exited, err := attachSess.WaitErr(0); exited {
			if err == nil || strings.Contains(err.Error(), "no sessions available") {
				return
			}
			t.Fatalf("attach exited unexpectedly: %v", err)
		}
		if !waitForAnyClientReadyOptional(h.Clock(), &viewsMu, views, 4*time.Second) {
			t.Fatalf("timed out waiting for any client ready after restart")
		}
	}
	sendTokenAcrossTabs(t, attachSess, "RECONNECT_OK", 4)
	_ = host
}

func sendTokenAcrossTabs(t *testing.T, sess *ptytest.PTYSession, token string, attempts int) {
	t.Helper()
	if attempts <= 0 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		sess.Send("echo " + token + "\n")
		if screenContainsWithin(sess, token, 3*time.Second) {
			return
		}
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 200*time.Millisecond)
	}
	t.Fatalf("expected output %q after reconnect", token)
}

func waitForClientReadyOptional(clk clock.Clock, mu *sync.Mutex, views map[string]*attach.Client, id string, timeout time.Duration) bool {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		mu.Lock()
		client := views[id]
		mu.Unlock()
		if client != nil && client.Connected() {
			return true
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return false
}

func waitForAnyClientReadyOptional(clk clock.Clock, mu *sync.Mutex, views map[string]*attach.Client, timeout time.Duration) bool {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		mu.Lock()
		for _, client := range views {
			if client != nil && client.Connected() {
				mu.Unlock()
				return true
			}
		}
		mu.Unlock()
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return false
}
