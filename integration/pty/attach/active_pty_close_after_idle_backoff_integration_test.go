//go:build integration
// +build integration

package integrationptyattach_test

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachDoesNotHammerAPIWhenActivePTYClosesAfterIdleTTL(t *testing.T) {
	var sessionsCount int64
	var wsClientCount int64

	h := newHarness(t, ptytest.WithRequestHook(func(r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			atomic.AddInt64(&sessionsCount, 1)
		case strings.HasSuffix(r.URL.Path, "/ws/client"):
			atomic.AddInt64(&wsClientCount, 1)
		}
	}))

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-1",
		SessionName: "alpha",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	host.SendCtrlL()
	host.Send("c")

	secondID, err := waitForSessionName(t, h.Clock(), h.Endpoint(), h.AccessToken(), "alpha-2", 5*time.Second)
	if err != nil {
		t.Fatalf("wait for second session: %v", err)
	}

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:   "host-1",
		Cols:        120,
		Rows:        30,
		InactiveTTL: 200 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitForClientReady(t, h.Clock(), &viewsMu, views, secondID, 3*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("p")
	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-1", 3*time.Second)

	h.Advance(1500 * time.Millisecond)

	host.SendCtrlL()
	host.Send("p")
	host.Send("exit\n")
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{secondID})

	startSessions := atomic.LoadInt64(&sessionsCount)
	startWS := atomic.LoadInt64(&wsClientCount)

	h.Advance(2 * time.Second)

	deltaSessions := atomic.LoadInt64(&sessionsCount) - startSessions
	deltaWS := atomic.LoadInt64(&wsClientCount) - startWS

	if deltaSessions > 3 || deltaWS > 3 {
		t.Fatalf("unexpected request churn after active pty close: sessions=%d ws=%d", deltaSessions, deltaWS)
	}
	_ = host
}
