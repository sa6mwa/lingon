package testclock

import (
	"pkt.systems/lingon/internal/clock"
	"time"
)

// Mock is a test clock with explicit advancement and joined callbacks.
type Mock interface {
	clock.Clock
	Add(time.Duration)
}

type synchronous struct {
	*clock.MockClock
	callbacks []callback
}

type callback struct {
	timer *clock.Timer
	fn    func()
}

// New constructs a synchronously advanced implementation of Mock.
func New() Mock { return &synchronous{MockClock: clock.NewMock()} }

func (c *synchronous) AfterFunc(d time.Duration, fn func()) *clock.Timer {
	timer := c.NewTimer(d)
	c.callbacks = append(c.callbacks, callback{timer: timer, fn: fn})
	return timer
}

func (c *synchronous) Add(d time.Duration) {
	c.MockClock.Add(d)
	for _, cb := range c.callbacks {
		select {
		case <-cb.timer.C:
			cb.fn()
		default:
		}
	}
}
