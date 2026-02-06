package host_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relay"
)

func TestHostHonorsRetryAfter(t *testing.T) {
	const retryAfter = time.Second
	cfg := relay.ConnectLimitConfig{
		Disable:  false,
		Burst:    1,
		Count:    1,
		Window:   time.Second,
		Headroom: 1,
	}

	clk := clock.NewMock()
	var mu sync.Mutex
	var attempts []time.Time
	h := newHarness(t,
		ptytest.WithClock(clk),
		ptytest.WithConnectLimiter(cfg),
		ptytest.WithRequestHook(func(r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/ws/host") {
				mu.Lock()
				attempts = append(attempts, ptytest.Now(clk))
				mu.Unlock()
			}
		}),
	)

	_ = h.StartHost(ptytest.HostOptions{SessionID: "session_retry_host"})

	waitForAttempts(t, clk, &mu, &attempts, 2, 8*time.Second)

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

func waitForAttempts(t *testing.T, clk clock.Clock, mu *sync.Mutex, attempts *[]time.Time, want int, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		mu.Lock()
		count := len(*attempts)
		mu.Unlock()
		if count >= want {
			return
		}
		ptytest.Advance(clk, 20*time.Millisecond)
	}
	mu.Lock()
	count := len(*attempts)
	mu.Unlock()
	t.Fatalf("expected %d attempts, got %d", want, count)
}
