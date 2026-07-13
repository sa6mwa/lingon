package backoff

import (
	cryptorand "crypto/rand"
	"math"
	"math/big"
	"time"
)

// Jitter supplies a randomized delay from zero through the supplied maximum.
// It is injectable so callers can make retry timing deterministic in tests.
type Jitter func(max time.Duration) time.Duration

// Policy defines exponential backoff behavior.
type Policy struct {
	Base   time.Duration
	Factor float64
	Max    time.Duration
	// Jitter is added to each reconnect delay to spread simultaneous retries.
	Jitter time.Duration
}

// DefaultPolicy is tuned to avoid aggressive reconnect loops.
var DefaultPolicy = Policy{
	Base:   time.Second,
	Factor: 2,
	Max:    1 * time.Minute,
	Jitter: 10 * time.Second,
}

// Next returns the delay for the given attempt (0-based).
func (p Policy) Next(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if p.Base <= 0 {
		p.Base = time.Second
	}
	if p.Factor <= 1 || math.IsNaN(p.Factor) || math.IsInf(p.Factor, 0) {
		p.Factor = 2
	}
	if p.Max <= 0 {
		p.Max = 3 * time.Minute
	}
	if p.Base >= p.Max {
		return p.Max
	}

	// Clamp attempt before exponentiation so float->duration conversion never overflows.
	rawMaxAttempt := math.Log(float64(p.Max)/float64(p.Base)) / math.Log(p.Factor)
	if math.IsNaN(rawMaxAttempt) || math.IsInf(rawMaxAttempt, 0) {
		return p.Max
	}
	maxAttempt := int(math.Floor(rawMaxAttempt))
	if maxAttempt < 0 {
		return p.Max
	}
	if attempt > maxAttempt {
		return p.Max
	}

	growth := math.Pow(p.Factor, float64(attempt))
	if math.IsNaN(growth) || math.IsInf(growth, 0) {
		return p.Max
	}
	delayFloat := float64(p.Base) * growth
	if math.IsNaN(delayFloat) || math.IsInf(delayFloat, 0) {
		return p.Max
	}
	delay := time.Duration(delayFloat)
	if delay <= 0 || delay > p.Max {
		return p.Max
	}
	return delay
}

// WithJitter adds the policy's bounded reconnect jitter to delay.
// A nil sampler uses crypto-secure randomness.
func (p Policy) WithJitter(delay time.Duration, sampler Jitter) time.Duration {
	if delay <= 0 || p.Jitter <= 0 {
		return delay
	}
	if sampler == nil {
		sampler = RandomJitter
	}
	jitter := sampler(p.Jitter)
	if jitter < 0 {
		jitter = 0
	}
	if jitter > p.Jitter {
		jitter = p.Jitter
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if delay > maxDuration-jitter {
		return maxDuration
	}
	return delay + jitter
}

// RandomJitter returns a crypto-random duration in the inclusive range [0, max].
func RandomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	limit := big.NewInt(int64(max))
	limit.Add(limit, big.NewInt(1))
	value, err := cryptorand.Int(cryptorand.Reader, limit)
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}
