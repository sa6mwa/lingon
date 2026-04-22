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
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachDoesNotHammerAfterStreamTimeoutAndPTYClose(t *testing.T) {
	var sessionsCount int64
	var wsClientCount int64
	var sessionsMu sync.Mutex
	var sessionsAt []time.Time

	clk := clock.New()
	h := newHarness(
		t,
		ptytest.WithClock(clk),
		ptytest.WithRequestHook(func(r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/sessions"):
				atomic.AddInt64(&sessionsCount, 1)
				sessionsMu.Lock()
				sessionsAt = append(sessionsAt, ptytest.Now(clk))
				sessionsMu.Unlock()
			case strings.HasSuffix(r.URL.Path, "/ws/client"):
				atomic.AddInt64(&wsClientCount, 1)
			}
		}),
	)

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

	host.SendCtrlL()
	host.Send("n")

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:   secondID,
		Cols:        120,
		Rows:        30,
		InactiveTTL: 200 * time.Millisecond,
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})

	waitForClientReady(t, h.Clock(), &viewsMu, views, secondID, 3*time.Second)

	viewsMu.Lock()
	client := views[secondID]
	viewsMu.Unlock()
	if client != nil {
		client.Close("timeout")
	}

	h.Advance(800 * time.Millisecond)

	host.SendCtrlL()
	host.Send("Q")

	startSessions := atomic.LoadInt64(&sessionsCount)
	startWS := atomic.LoadInt64(&wsClientCount)

	h.Advance(2 * time.Second)

	deltaSessions := atomic.LoadInt64(&sessionsCount) - startSessions
	deltaWS := atomic.LoadInt64(&wsClientCount) - startWS

	if deltaSessions > 4 || deltaWS > 4 {
		sessionsMu.Lock()
		observed := append([]time.Time(nil), sessionsAt...)
		sessionsMu.Unlock()
		t.Fatalf("unexpected request churn after stream timeout + pty close: sessions=%d ws=%d timestamps=%v", deltaSessions, deltaWS, observed)
	}
	_ = attachSess
}
