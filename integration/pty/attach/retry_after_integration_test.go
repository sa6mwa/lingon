//go:build integration
// +build integration

package integrationptyattach_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relay"
)

func TestAttachHonorsRetryAfter(t *testing.T) {
	const retryAfter = 2 * time.Second
	cfg := relay.ConnectLimitConfig{
		Disable:  false,
		Burst:    4,
		Count:    2,
		Window:   4 * time.Second,
		Headroom: 1,
	}

	var mu sync.Mutex
	var attempts []time.Time
	clk := clock.New()
	h := newHarness(t,
		ptytest.WithClock(clk),
		ptytest.WithConnectLimiter(cfg),
		ptytest.WithRequestHook(func(r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/ws/client") {
				mu.Lock()
				attempts = append(attempts, ptytest.Now(clk))
				mu.Unlock()
			}
		}),
	)

	sessionID := "session_retry_attach"
	_ = h.StartHost(ptytest.HostOptions{SessionID: sessionID})
	waitForHost(t, h, sessionID, 3*time.Second)

	drainConnectTokens(t, h.Endpoint()+"/ws/client", 2)

	mu.Lock()
	attempts = nil
	mu.Unlock()

	_ = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: sessionID})

	waitUntil(t, h.Clock(), 8*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(attempts) >= 2
	})

	mu.Lock()
	first := attempts[0]
	second := attempts[1]
	mu.Unlock()

	delta := second.Sub(first)
	minDelay := retryAfter - 200*time.Millisecond
	if delta < minDelay {
		t.Fatalf("expected retry-after >= %v, got %v", minDelay, delta)
	}
}

func drainConnectTokens(t *testing.T, endpoint string, count int) {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // test harness TLS
			DisableKeepAlives: true,
		},
		Timeout: time.Second,
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}
	for i := 0; i < count; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			t.Fatalf("drain request: %v", err)
		}
		resp, err := client.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("drain request failed: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("unexpected rate limit while draining tokens")
		}
	}
}
