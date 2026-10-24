package testclock

import (
	"sync"
	"time"

	"pkt.systems/lingon/internal/clock"
)

// Mock is a test clock with explicit advancement and joined callbacks.
type Mock interface {
	clock.Clock
	Add(time.Duration)
}

type synchronous struct {
	*clock.MockClock
	advanceMu sync.Mutex
	mu        sync.Mutex
	callbacks []callback
}

type callback struct {
	timer *clock.Timer
	fn    func()
}

// New constructs a synchronously advanced implementation of Mock.
func New() Mock { return &synchronous{MockClock: clock.NewMock()} }

func (c *synchronous) AfterFunc(d time.Duration, fn func()) *clock.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := c.NewTimer(d)
	c.callbacks = append(c.callbacks, callback{timer: timer, fn: fn})
	return timer
}

func (c *synchronous) Add(d time.Duration) {
	c.advanceMu.Lock()
	defer c.advanceMu.Unlock()
	c.MockClock.Add(d)
	for {
		c.mu.Lock()
		var ready []func()
		for _, cb := range c.callbacks {
			select {
			case <-cb.timer.C:
				ready = append(ready, cb.fn)
			default:
			}
		}
		c.mu.Unlock()
		if len(ready) == 0 {
			return
		}
		for _, fn := range ready {
			fn()
		}
		// Include immediately due timers installed by callbacks, without
		// moving time or depending on another goroutine being scheduled.
		c.MockClock.Add(0)
	}
}
