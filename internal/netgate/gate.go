package netgate

import (
	"context"
	"sync"
	"time"

	"pkt.systems/lingon/internal/clock"
)

// Gate blocks network activity for a bounded duration.
type Gate struct {
	mu           sync.Mutex
	blockedUntil time.Time
	clock        clock.Clock
}

// New returns a new gate using the provided clock.
func New(clk clock.Clock) *Gate {
	if clk == nil {
		clk = clock.New()
	}
	return &Gate{clock: clk}
}

// BlockFor blocks the gate for at least the provided duration.
func (g *Gate) BlockFor(d time.Duration) {
	if g == nil {
		return
	}
	if d <= 0 {
		g.Allow()
		return
	}
	until := g.clock.Now().Add(d)
	g.mu.Lock()
	if until.After(g.blockedUntil) {
		g.blockedUntil = until
	}
	g.mu.Unlock()
}

// Allow clears any existing block.
func (g *Gate) Allow() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.blockedUntil = time.Time{}
	g.mu.Unlock()
}

// Allowed reports whether the gate is open.
func (g *Gate) Allowed() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blockedUntil.IsZero() || g.clock.Now().After(g.blockedUntil)
}

// Remaining reports the remaining block duration, if any.
func (g *Gate) Remaining() time.Duration {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blockedUntil.IsZero() {
		return 0
	}
	remaining := g.blockedUntil.Sub(g.clock.Now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Wait blocks until the gate is allowed or the context is done.
func (g *Gate) Wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	for {
		remaining := g.Remaining()
		if remaining <= 0 {
			return nil
		}
		timer := g.clock.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
