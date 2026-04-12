package attach_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachFastReadyDoesNotLeaveLoadingBanner(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "loading-fast",
		SessionName: "loading-fast",
		Shell:       "/bin/sh",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"loading-fast"})

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "loading-fast",
		Cols:      100,
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
	t.Cleanup(attachSess.Cancel)

	waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	ptytest.Advance(h.Clock(), 4*time.Second)
	attachSess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connected to ") {
			return fmt.Errorf("expected connected banner cleared, row=%q", row)
		}
		if strings.Contains(row, "loading from relay") {
			return fmt.Errorf("expected loading banner cleared after ready, row=%q", row)
		}
		return nil
	})
}
