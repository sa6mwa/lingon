//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachLargeResizeBurst(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-resize",
		Shell:     "/bin/cat",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-resize"})

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-resize",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	sendLineUntilScreenContainsRealTime(t, attachSess, "RESIZE_BURST_READY", 3*time.Second)

	sizes := [][2]int{{200, 60}, {120, 30}, {160, 50}, {80, 24}, {240, 80}}
	for i, size := range sizes {
		host.Resize(size[0], size[1])
		time.Sleep(75 * time.Millisecond)
		token := "RESIZE_BURST_" + string(rune('A'+i))
		attachSess.Send(token + "\n")
		if !screenContainsWithinRealTime(attachSess, token, 3*time.Second) {
			t.Fatalf("expected output after resize %v\nhost screen:\n%s\nattach screen:\n%s", size, host.Screen(), attachSess.Screen())
		}
	}
}

func sendLineUntilScreenContainsRealTime(t *testing.T, sess *ptytest.PTYSession, token string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	nextSend := time.Time{}
	for time.Now().Before(deadline) {
		if sess.Screen().Contains(token) {
			return
		}
		if !time.Now().Before(nextSend) {
			sess.Send(token + "\n")
			nextSend = time.Now().Add(100 * time.Millisecond)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for primer token %q\nscreen:\n%s", token, sess.Screen())
}

func TestMultiHostSwitchWhileReconnect(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Shell: shell, Cols: 120, Rows: 30})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "host-b", Shell: shell, Cols: 120, Rows: 30})

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 5*time.Second)
	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	count := len(ids)
	if count < 4 {
		t.Fatalf("expected >=4 sessions, got %d", count)
	}

	attachA, activeA, viewsA := startTrackedAttach(t, h, "host-a")
	attachB, activeB, viewsB := startTrackedAttach(t, h, "host-b")
	t.Cleanup(attachA.Cancel)
	t.Cleanup(attachB.Cancel)

	primeTabsByCountWithActive(t, attachA, count, h.Clock(), activeA)
	primeTabsByCountWithActive(t, attachB, count, h.Clock(), activeB)

	h.StopServer()

	attachA.SendCtrlL()
	attachA.Send("n")
	attachB.SendCtrlL()
	attachB.Send("n")
	h.Advance(300 * time.Millisecond)

	h.RestartServer()
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), count, 6*time.Second)
	if _, err := fetchSessionIDs(h.Endpoint(), h.AccessToken()); err != nil {
		t.Fatalf("fetch sessions after restart: %v", err)
	}
	reconnectReadyTimeout := 10 * time.Second
	attachA, activeA, viewsA = ensureTrackedAttachReady(
		t, h, attachA, activeA, viewsA, "attachA", "host-a", reconnectReadyTimeout,
	)
	attachB, activeB, viewsB = ensureTrackedAttachReady(
		t, h, attachB, activeB, viewsB, "attachB", "host-b", reconnectReadyTimeout,
	)

	_ = cycleSendTokensWithActive(t, attachA, 1, "RECONNECT_MULTI_A", h.Clock(), activeA.mu, activeA.id, activeA.viewsMu, viewsA)
	_ = cycleSendTokensWithActive(t, attachB, 1, "RECONNECT_MULTI_B", h.Clock(), activeB.mu, activeB.id, activeB.viewsMu, viewsB)
	_ = hostA
	_ = hostB
}

func TestAttachScrollbackDuringDisconnect(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-scroll",
		Shell:     writeAttachPromptShell(t),
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-scroll"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before scrollback test")
	}

	attachSess, active, views := startTrackedAttach(t, h, "host-scroll")
	t.Cleanup(attachSess.Cancel)

	attachSess.Send("for i in $(seq 1 200); do echo SCROLL_$i; done\n")
	if !screenContainsWithin(attachSess, "SCROLL_200", 5*time.Second) {
		t.Fatalf("expected scroll output")
	}

	attachSess.SendCtrlL()
	attachSess.Send("[")
	attachSess.SendBytes([]byte{0x1b, '[', '5', '~'})
	h.Advance(200 * time.Millisecond)

	h.StopServer()
	h.Advance(300 * time.Millisecond)

	attachSess.Send("q")
	h.RestartServer()
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-scroll"})
	attachSess, _, _ = ensureTrackedAttachReady(
		t, h, attachSess, active, views, "attach-scroll", "host-scroll", 10*time.Second,
	)
	attachSess.Send("q")
	if !screenContainsWithin(attachSess, "PROMPT>", 2*time.Second) {
		t.Fatalf("expected prompt restored after leaving scrollback on reconnect, got:\n%s", attachSess.Screen().String())
	}

	attachSess.Send("echo SCROLL_BACK\n")
	if !screenContainsWithin(attachSess, "SCROLL_BACK", 5*time.Second) {
		t.Fatalf("expected attach after scrollback + reconnect")
	}
	_ = host
}
