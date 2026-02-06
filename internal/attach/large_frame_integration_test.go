package attach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachHandlesLargeSnapshotFrames(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-big",
		Shell:     shell,
		Cols:      240,
		Rows:      80,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-big"})

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-big",
		Cols:            240,
		Rows:            80,
		InactiveTTL:     5 * time.Second,
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

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo LARGE_FRAME_OK\n")
	if !screenContainsWithin(attachSess, "LARGE_FRAME_OK", 3*time.Second) {
		t.Fatalf("expected attach output after large snapshot connect")
	}
	_ = host
}
