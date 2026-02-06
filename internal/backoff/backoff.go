package backoff

import (
	"math"
	"time"
)

// Policy defines exponential backoff behavior.
type Policy struct {
	Base   time.Duration
	Factor float64
	Max    time.Duration
}

// DefaultPolicy is tuned to avoid aggressive reconnect loops.
var DefaultPolicy = Policy{
	Base:   time.Second,
	Factor: 2,
	Max:    1 * time.Minute,
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
