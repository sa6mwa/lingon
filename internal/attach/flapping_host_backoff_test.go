package attach_test

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

func TestAttachBackoffOnFlappingHost(t *testing.T) {
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
	if _, err := os.Stat("/bin/cat"); err == nil {
		shell = "/bin/cat"
	}

	sessionID := "flap-session"
	_ = h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	startSessions := atomic.LoadInt64(&sessionsCount)
	startWS := atomic.LoadInt64(&wsClientCount)

	var viewMu sync.Mutex
	var view *attach.Client
	_ = h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      24,
		OnView: func(id string, client *attach.Client) {
			if id != sessionID {
				return
			}
			viewMu.Lock()
			view = client
			viewMu.Unlock()
		},
	})

	deadline := ptytest.Now(h.Clock()).Add(2 * time.Second)
	for ptytest.Now(h.Clock()).Before(deadline) {
		viewMu.Lock()
		client := view
		viewMu.Unlock()
		if client != nil && client.Connected() {
			client.Close("flap")
		}
		h.Advance(50 * time.Millisecond)
	}

	h.Advance(2 * time.Second)

	sessions := atomic.LoadInt64(&sessionsCount) - startSessions
	wsClients := atomic.LoadInt64(&wsClientCount) - startWS
	if wsClients > 10 || sessions > 10 {
		t.Fatalf("expected throttled reconnects, got ws=%d sessions=%d", wsClients, sessions)
	}

}
