package mvu

import (
	"context"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestNextExpiryDelay(t *testing.T) {
	now := time.Unix(100, 0)
	state := State{
		ConnectionExpiresAt: now.Add(3 * time.Second),
		WallExpiresAt:       now.Add(5 * time.Second),
		TabBarExpiresAt:     now.Add(2 * time.Second),
	}
	if got := NextExpiryDelay(state, now); got != 2*time.Second {
		t.Fatalf("expected nearest expiry 2s, got %v", got)
	}
	if got := NextExpiryDelay(state, now.Add(3*time.Second)); got != 2*time.Second {
		t.Fatalf("expected wall expiry remaining 2s, got %v", got)
	}
	if got := NextExpiryDelay(State{}, now); got != 0 {
		t.Fatalf("expected zero when no expiries, got %v", got)
	}
}

func TestEffectSchedulerScheduleReplaceStopAll(t *testing.T) {
	mock := clock.NewMock()
	s := NewEffectScheduler(mock)
	ctx := context.Background()
	count := 0

	s.Schedule(ctx, EffectKeyStateExpiry, 2*time.Second, func() {
		count++
	})
	mock.Add(time.Second)
	if count != 0 {
		t.Fatalf("timer fired too early")
	}

	s.Schedule(ctx, EffectKeyStateExpiry, 3*time.Second, func() {
		count += 10
	})
	mock.Add(2 * time.Second)
	if count != 0 {
		t.Fatalf("replaced timer should not have fired yet")
	}
	mock.Add(time.Second)
	if count != 10 {
		t.Fatalf("expected replaced callback to fire once, got %d", count)
	}

	s.Schedule(ctx, EffectKeyTabAutoHide, 2*time.Second, func() {
		count += 100
	})
	s.StopAll()
	mock.Add(3 * time.Second)
	if count != 10 {
		t.Fatalf("expected StopAll to cancel timers, got %d", count)
	}
}
