package attach_test

import (
	"os"
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

	if !screenContainsWithin(attachSess, "Not connected", 5*time.Second) {
		t.Fatalf("expected disconnect modal after server stop")
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")

	h.Advance(300 * time.Millisecond)
	assertNoBlink(t, attachSess, "Not connected", 2*time.Second, 200*time.Millisecond)
}

func assertNoBlink(t *testing.T, sess *ptytest.PTYSession, needle string, duration, interval time.Duration) {
	t.Helper()
	clk := sess.Clock()
	deadline := ptytest.Now(clk).Add(duration)
	seenOn := false
	seenOff := false
	for ptytest.Now(clk).Before(deadline) {
		if sess.Screen().Contains(needle) {
			seenOn = true
		} else {
			seenOff = true
		}
		if seenOn && seenOff {
			t.Fatalf("disconnect modal blinked while switching tabs")
		}
		ptytest.Advance(clk, interval)
	}
	if !seenOn {
		t.Fatalf("disconnect modal not visible during sampling window")
	}
}
