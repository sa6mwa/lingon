package ptytest

import (
	"runtime"
	"time"

	"pkt.systems/lingon/internal/clock"
)

type advancingClock interface {
	clock.Clock
	Add(time.Duration)
}

// Now returns the current time for the provided clock (or time.Now if nil).
func Now(c clock.Clock) time.Time {
	if c == nil {
		return time.Now()
	}
	return c.Now()
}

// Advance moves time forward for mock clocks or sleeps for real clocks.
func Advance(c clock.Clock, d time.Duration) {
	if d <= 0 {
		return
	}
	if c == nil {
		time.Sleep(d)
		return
	}
	if adv, ok := c.(advancingClock); ok {
		adv.Add(d)
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
		return
	}
	c.Sleep(d)
}
