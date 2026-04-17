package attach_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachDisconnectModalDoesNotBlinkOnTabSwitchWhenServerStops(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})
	host.SendCtrlL()
	host.Send("c")
	secondID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host-1", 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
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

	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-1", 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitForClientReady(t, h.Clock(), &viewsMu, views, secondID, 3*time.Second)
	attachSess.SendCtrlL()
	attachSess.Send("p")
	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-1", 3*time.Second)

	h.StopServer()

	sawDisconnectModal := screenContainsWithin(attachSess, "Not connected", 3*time.Second)
	if !sawDisconnectModal {
		_ = screenContainsWithin(attachSess, "no sessions available", 2*time.Second)
		_ = screenContainsWithin(attachSess, "Waiting for sessions", 2*time.Second)
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")

	h.Advance(300 * time.Millisecond)
	if sawDisconnectModal {
		assertNoDisconnectOverlayBlink(t, attachSess, 2*time.Second, 200*time.Millisecond)
	}
	_ = attachSessionUsable(t, attachSess)
}

func assertNoDisconnectOverlayBlink(t *testing.T, sess *ptytest.PTYSession, duration, interval time.Duration) {
	t.Helper()
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(duration)
	seenOverlay := false
	seenUnderlying := false
	for ptytest.Now(clk).Before(deadline) {
		row := sess.Screen().Row(0)
		if strings.Contains(row, "Not connected") || strings.Contains(row, "Waiting for sessions") || strings.Contains(row, "no sessions available") {
			seenOverlay = true
		} else {
			seenUnderlying = true
		}
		if seenOverlay && seenUnderlying {
			t.Fatalf("disconnect overlay blinked while switching tabs; row=%q", row)
		}
		ptytest.Advance(clk, interval)
	}
}
