package relay

import (
	"testing"
	"time"
)

func TestConnectLimiterRetryAfter(t *testing.T) {
	limiter := NewConnectLimiter(ConnectLimitConfig{
		Burst:    40,
		Count:    200,
		Window:   30 * time.Minute,
		Headroom: 3,
	})
	if limiter == nil {
		t.Fatalf("expected limiter")
	}

	start := time.Now().Add(time.Second)

	for i := 0; i < 37; i++ {
		allowed, retry := limiter.Allow(start)
		if !allowed || retry != 0 {
			t.Fatalf("expected allowed at %d, got allowed=%v retry=%v", i, allowed, retry)
		}
	}

	allowed, retry := limiter.Allow(start)
	if allowed {
		t.Fatalf("expected throttled at headroom")
	}
	if retry <= 0 {
		t.Fatalf("expected retry-after to be set")
	}

	allowed, _ = limiter.Allow(start.Add(9 * time.Second))
	if !allowed {
		t.Fatalf("expected allow after minimal refill")
	}
	allowed, _ = limiter.Allow(start.Add(9 * time.Second))
	if allowed {
		t.Fatalf("expected throttle after minimal refill spend")
	}

	allowed, _ = limiter.Allow(start.Add(90 * time.Second))
	if !allowed {
		t.Fatalf("expected allow after refill")
	}

	allowed, retry = limiter.Allow(start.Add(90 * time.Second))
	if !allowed || retry != 0 {
		t.Fatalf("expected allow after refill, got allowed=%v retry=%v", allowed, retry)
	}

	allowedCount := 0
	for i := 0; i < 50; i++ {
		allowed, retry = limiter.Allow(start.Add(90 * time.Second))
		if allowed {
			allowedCount++
			continue
		}
		if retry <= 0 {
			t.Fatalf("expected retry-after after headroom spend")
		}
		break
	}
	if allowedCount == 0 {
		t.Fatalf("expected at least one allow after refill")
	}
}
