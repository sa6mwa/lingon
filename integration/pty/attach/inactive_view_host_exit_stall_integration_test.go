//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachInactiveViewHostExitDoesNotStallReconnect(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	host.SendCtrlL()
	host.Send("c")
	host.SendCtrlL()
	host.Send("p")

	secondID := waitForNewSessionID(t, h.Clock(), h.Endpoint(), h.AccessToken(), "host-1", 5*time.Second)
	labelMap := sessionLabelMap(t, h.Endpoint(), h.AccessToken())
	secondLabel := labelMap[secondID]
	if secondLabel == "" {
		t.Fatalf("missing label for session %q", secondID)
	}

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     300 * time.Millisecond,
		RefreshInterval: 150 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
		OnViewClosed: func(sessionID string, _ bool, _ bool) {
			viewsMu.Lock()
			delete(views, sessionID)
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

	waitUntil(t, h.Clock(), 3*time.Second, func() bool {
		viewsMu.Lock()
		_, ok := views[secondID]
		viewsMu.Unlock()
		return !ok
	})

	host.SendCtrlL()
	host.Send("n")
	host.SendBytes([]byte{0x04})

	waitForSessionRemovalByID(t, h.Clock(), h.Endpoint(), h.AccessToken(), secondID, 5*time.Second)

	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("reconnecting in 0s") {
			return fmt.Errorf("expected reconnect overlay to clear; screen:\n%s", screen.String())
		}
		return nil
	})
	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if secondLabel != "" && screen.Contains(secondLabel) {
			return fmt.Errorf("expected removed tab %q to disappear, row=%q", secondLabel, row)
		}
		return nil
	})

	if done, err := attachSess.WaitErr(200 * time.Millisecond); done {
		t.Fatalf("attach exited unexpectedly: %v", err)
	}
	if done, err := host.WaitErr(200 * time.Millisecond); done {
		t.Fatalf("host exited unexpectedly: %v", err)
	}
}
