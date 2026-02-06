package relay

import (
	"math"
	"sync"
	"time"
)

// ConnectLimiter enforces a global connection rate limit.
type ConnectLimiter struct {
	mu       sync.Mutex
	tokens   float64
	last     time.Time
	burst    float64
	rate     float64
	headroom float64
	disabled bool
}

// ConnectLimitConfig configures the global connection limiter.
type ConnectLimitConfig struct {
	Disable  bool
	Burst    int
	Count    int
	Window   time.Duration
	Headroom int
}

// NewConnectLimiter constructs a limiter from config.
func NewConnectLimiter(cfg ConnectLimitConfig) *ConnectLimiter {
	if cfg.Disable {
		return &ConnectLimiter{disabled: true}
	}
	if cfg.Burst <= 0 || cfg.Count <= 0 || cfg.Window <= 0 {
		return &ConnectLimiter{disabled: true}
	}
	headroom := cfg.Headroom
	if headroom <= 0 {
		headroom = 3
	}
	rate := float64(cfg.Count) / cfg.Window.Seconds()
	return &ConnectLimiter{
		tokens:   float64(cfg.Burst),
		last:     time.Now(),
		burst:    float64(cfg.Burst),
		rate:     rate,
		headroom: float64(headroom),
	}
}

// Allow reports whether a connection is permitted and returns retry-after when not.
func (l *ConnectLimiter) Allow(now time.Time) (bool, time.Duration) {
	if l == nil || l.disabled {
		return true, 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	elapsed := now.Sub(l.last)
	if elapsed > 0 {
		l.tokens += elapsed.Seconds() * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
	}

	need := l.headroom + 1
	if l.tokens >= need {
		l.tokens--
		return true, 0
	}
	if l.rate <= 0 {
		return false, 30 * time.Minute
	}
	deficit := need - l.tokens
	seconds := math.Ceil(deficit / l.rate)
	if seconds < 1 {
		seconds = 1
	}
	return false, time.Duration(seconds) * time.Second
}
