package attach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachLargeResizeBurst(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-resize",
		Shell:     shell,
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

	sizes := [][2]int{{200, 60}, {120, 30}, {160, 50}, {80, 24}, {240, 80}}
	for i, size := range sizes {
		host.Resize(size[0], size[1])
		token := "RESIZE_BURST_" + string(rune('A'+i))
		attachSess.Send("echo " + token + "\n")
		if !screenContainsWithin(attachSess, token, 3*time.Second) {
			t.Fatalf("expected output after resize %v", size)
		}
	}
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

	var activeMuA sync.Mutex
	activeIDA := ""
	var viewsMuA sync.Mutex
	viewsA := make(map[string]*attach.Client)
	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMuA.Lock()
			activeIDA = id
			activeMuA.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMuA.Lock()
			viewsA[id] = client
			viewsMuA.Unlock()
		},
	})
	var activeMuB sync.Mutex
	activeIDB := ""
	var viewsMuB sync.Mutex
	viewsB := make(map[string]*attach.Client)
	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-b",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMuB.Lock()
			activeIDB = id
			activeMuB.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMuB.Lock()
			viewsB[id] = client
			viewsMuB.Unlock()
		},
	})
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuA, &activeIDA, &viewsMuA, viewsA, "", 3*time.Second)
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuB, &activeIDB, &viewsMuB, viewsB, "", 3*time.Second)

	primeTabsByCount(t, attachA, count)
	primeTabsByCount(t, attachB, count)

	h.StopServer()

	attachA.SendCtrlL()
	attachA.Send("n")
	attachB.SendCtrlL()
	attachB.Send("n")
	h.Advance(300 * time.Millisecond)

	h.RestartServer()
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), count, 6*time.Second)
	reconnectReadyTimeout := 10 * time.Second
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuA, &activeIDA, &viewsMuA, viewsA, "", reconnectReadyTimeout)
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuB, &activeIDB, &viewsMuB, viewsB, "", reconnectReadyTimeout)

	sendTokenAcrossTabsPhase1(t, attachA, "RECONNECT_MULTI_A", count+1)
	sendTokenAcrossTabsPhase1(t, attachB, "RECONNECT_MULTI_B", count+1)
	_ = hostA
	_ = hostB
}

func TestAttachScrollbackDuringDisconnect(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-scroll",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-scroll"})

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-scroll",
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
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("q")
	h.Advance(100 * time.Millisecond)

	attachSess.Send("echo SCROLL_BACK\n")
	if !screenContainsWithin(attachSess, "SCROLL_BACK", 5*time.Second) {
		t.Fatalf("expected attach after scrollback + reconnect")
	}
	_ = host
}

func sendTokenAcrossTabsPhase1(t *testing.T, sess *ptytest.PTYSession, token string, attempts int) {
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
