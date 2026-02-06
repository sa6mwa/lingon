package mvu

import (
	"context"
	"sync"
	"time"

	"pkt.systems/lingon/internal/clock"
)

const (
	// EffectKeyStateExpiry identifies the timer that clears expiring overlays/banners.
	EffectKeyStateExpiry = "state-expiry"
	// EffectKeyTabAutoHide identifies the timer that hides row-1 tab overlays.
	EffectKeyTabAutoHide = "tab-autohide"
)

// NextExpiryDelay returns the nearest future state expiry delay.
func NextExpiryDelay(state State, now time.Time) time.Duration {
	next := time.Time{}
	for _, expiresAt := range []time.Time{
		state.ConnectionExpiresAt,
		state.WallExpiresAt,
		state.TabBarExpiresAt,
	} {
		if expiresAt.IsZero() || !expiresAt.After(now) {
			continue
		}
		if next.IsZero() || expiresAt.Before(next) {
			next = expiresAt
		}
	}
	if next.IsZero() {
		return 0
	}
	return next.Sub(now)
}

// EffectScheduler owns timer lifecycle for MVU-driven redraw effects.
type EffectScheduler struct {
	mu     sync.Mutex
	clock  clock.Clock
	timers map[string]*clock.Timer
}

// NewEffectScheduler constructs an effect scheduler.
func NewEffectScheduler(clk clock.Clock) *EffectScheduler {
	if clk == nil {
		clk = clock.New()
	}
	return &EffectScheduler{
		clock:  clk,
		timers: make(map[string]*clock.Timer),
	}
}

// SetClock replaces the scheduler clock.
func (s *EffectScheduler) SetClock(clk clock.Clock) {
	if s == nil || clk == nil {
		return
	}
	s.mu.Lock()
	s.clock = clk
	s.mu.Unlock()
}

// Schedule replaces any existing timer for key and fires fn after delay.
func (s *EffectScheduler) Schedule(ctx context.Context, key string, delay time.Duration, fn func()) {
	if s == nil || key == "" {
		return
	}
	if delay <= 0 || fn == nil {
		s.Stop(key)
		return
	}
	s.mu.Lock()
	if t := s.timers[key]; t != nil {
		_ = t.Stop()
	}
	clk := s.clock
	if clk == nil {
		clk = clock.New()
		s.clock = clk
	}
	s.timers[key] = clk.AfterFunc(delay, func() {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		fn()
	})
	s.mu.Unlock()
}

// Stop cancels a scheduled effect.
func (s *EffectScheduler) Stop(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	if t := s.timers[key]; t != nil {
		_ = t.Stop()
		delete(s.timers, key)
	}
	s.mu.Unlock()
}

// StopAll cancels all scheduled effects.
func (s *EffectScheduler) StopAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for key, t := range s.timers {
		if t != nil {
			_ = t.Stop()
		}
		delete(s.timers, key)
	}
	s.mu.Unlock()
}
