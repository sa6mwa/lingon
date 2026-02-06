package attach_test

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRemovesTabAfterInactiveViewTimeoutAndHostExit(t *testing.T) {
	restoreTabDelay := mvu.SetTabBarAutoHideDelay(24 * time.Hour)
	defer restoreTabDelay()

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
	labelMap := sessionLabelMap(t, h.Endpoint(), h.AccessToken())
	primaryLabel := labelMap["host-1"]
	if primaryLabel == "" {
		t.Fatalf("missing label for session %q", "host-1")
	}
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
		InactiveTTL:     time.Hour,
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

	attachSess.Send("sleep 1000\n")

	viewsMu.Lock()
	inactiveClient := views[secondID]
	viewsMu.Unlock()
	if inactiveClient == nil {
		t.Fatalf("missing inactive client for %q", secondID)
	}
	inactiveClient.Close("timeout")

	waitUntil(t, h.Clock(), 4*time.Second, func() bool {
		viewsMu.Lock()
		client := views[secondID]
		viewsMu.Unlock()
		return client != nil && !client.Connected()
	})

	attachSess.Wait(4 * time.Second)

	attachSess.Eventually(4*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.Row(0), "connected to") {
			return fmt.Errorf("waiting for connection banner to clear")
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("b")
	attachSess.SendCtrlL()
	attachSess.Send("b")
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, primaryLabel) || !strings.Contains(row, secondLabel) {
			return fmt.Errorf("expected tab bar with %q and %q, row=%q", primaryLabel, secondLabel, row)
		}
		return nil
	})

	host.SendBytes([]byte{0x04})

	waitForSessionRemovalByID(t, h.Clock(), h.Endpoint(), h.AccessToken(), secondID, 5*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Send("echo PROBE\n")
	if !screenContainsWithin(host, "PROBE", 2*time.Second) {
		t.Fatalf("expected probe to reach active host after tab switch")
	}

	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, primaryLabel) || strings.Contains(row, secondLabel) {
			return fmt.Errorf("expected removed tab %q to disappear, row=%q", secondLabel, row)
		}
		return nil
	})
}
