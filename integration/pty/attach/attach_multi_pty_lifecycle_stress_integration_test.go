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

func TestAttachMultiPTYLifecycleStress(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	views := make(map[string]*attach.Client)
	var viewsMu sync.Mutex
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     1 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
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

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	for i := 0; i < 6; i++ {
		token := fmt.Sprintf("STRESS_%d", i)
		found := false
		deadline := ptytest.Now(h.Clock()).Add(4 * time.Second)
		for ptytest.Now(h.Clock()).Before(deadline) {
			attachSess.Send("echo " + token + "\n")
			if screenContainsWithin(attachSess, token, 600*time.Millisecond) {
				found = true
				break
			}
			attachSess.SendCtrlL()
			attachSess.Send("n")
			h.Advance(120 * time.Millisecond)
			if next, ok := tryWaitForActiveSessionReady(h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 1200*time.Millisecond); ok {
				currentActive = next
			}
		}
		if !found {
			t.Fatalf("expected output %q", token)
		}
		if screenIsBlank(attachSess.Screen()) {
			t.Fatalf("screen went blank at step %d", i)
		}

		switch i {
		case 2:
			host.SendCtrlL()
			host.Send("c")
			waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)
		case 4:
			host.SendCtrlL()
			host.Send("n")
			host.SendBytes([]byte{0x04})
			waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)
		}
	}
}
