package clock

import (
	"time"

	bjclock "github.com/benbjohnson/clock"
)

// Clock provides time controls for production and tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	AfterFunc(time.Duration, func()) *Timer
	Sleep(time.Duration)
	NewTicker(time.Duration) *Ticker
	NewTimer(time.Duration) *Timer
}

// Timer wraps a clock timer with a channel and stop/reset controls.
type Timer struct {
	C     <-chan time.Time
	stop  func() bool
	reset func(time.Duration) bool
}

// Stop stops the timer.
func (t *Timer) Stop() bool {
	if t == nil || t.stop == nil {
		return false
	}
	return t.stop()
}

// Reset resets the timer to the new duration.
func (t *Timer) Reset(d time.Duration) bool {
	if t == nil || t.reset == nil {
		return false
	}
	return t.reset(d)
}

// Ticker wraps a clock ticker with a channel and controls.
type Ticker struct {
	C     <-chan time.Time
	stop  func()
	reset func(time.Duration)
}

// Stop stops the ticker.
func (t *Ticker) Stop() {
	if t == nil || t.stop == nil {
		return
	}
	t.stop()
}

// Reset resets the ticker to the new duration.
func (t *Ticker) Reset(d time.Duration) {
	if t == nil || t.reset == nil {
		return
	}
	t.reset(d)
}

// RealClock uses the system clock.
type RealClock struct {
	clock bjclock.Clock
}

// New returns a system-backed clock.
func New() Clock {
	return &RealClock{clock: bjclock.New()}
}

// Now returns the current time.
func (c *RealClock) Now() time.Time {
	return c.clock.Now()
}

// After waits for the duration and returns a channel.
func (c *RealClock) After(d time.Duration) <-chan time.Time {
	return c.clock.After(d)
}

// AfterFunc waits for the duration then invokes f.
func (c *RealClock) AfterFunc(d time.Duration, f func()) *Timer {
	t := c.clock.AfterFunc(d, f)
	return &Timer{C: t.C, stop: t.Stop, reset: t.Reset}
}

// Sleep blocks for the duration.
func (c *RealClock) Sleep(d time.Duration) {
	c.clock.Sleep(d)
}

// NewTicker returns a ticker.
func (c *RealClock) NewTicker(d time.Duration) *Ticker {
	t := c.clock.Ticker(d)
	return &Ticker{C: t.C, stop: t.Stop, reset: t.Reset}
}

// NewTimer returns a timer.
func (c *RealClock) NewTimer(d time.Duration) *Timer {
	t := c.clock.Timer(d)
	return &Timer{C: t.C, stop: t.Stop, reset: t.Reset}
}

// MockClock is a controllable clock for tests.
type MockClock struct {
	clock *bjclock.Mock
}

// NewMock returns a controllable mock clock.
func NewMock() *MockClock {
	return &MockClock{clock: bjclock.NewMock()}
}

// Add advances the mock clock.
func (c *MockClock) Add(d time.Duration) {
	c.clock.Add(d)
}

// Now returns the current mock time.
func (c *MockClock) Now() time.Time {
	return c.clock.Now()
}

// After returns a channel that fires after the duration.
func (c *MockClock) After(d time.Duration) <-chan time.Time {
	return c.clock.After(d)
}

// AfterFunc waits for the duration then invokes f.
func (c *MockClock) AfterFunc(d time.Duration, f func()) *Timer {
	t := c.clock.AfterFunc(d, f)
	return &Timer{C: t.C, stop: t.Stop, reset: t.Reset}
}

// Sleep blocks for the duration in mock time.
func (c *MockClock) Sleep(d time.Duration) {
	c.clock.Sleep(d)
}

// NewTicker returns a ticker.
func (c *MockClock) NewTicker(d time.Duration) *Ticker {
	t := c.clock.Ticker(d)
	return &Ticker{C: t.C, stop: t.Stop, reset: t.Reset}
}

// NewTimer returns a timer.
func (c *MockClock) NewTimer(d time.Duration) *Timer {
	t := c.clock.Timer(d)
	return &Timer{C: t.C, stop: t.Stop, reset: t.Reset}
}
