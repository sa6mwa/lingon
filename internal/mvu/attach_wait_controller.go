package mvu

import "time"

// AttachWaitController owns wait-for-sessions policy state for attach UI.
type AttachWaitController struct {
	grace       time.Duration
	waiting     bool
	waitAllowed bool
	waitUntil   time.Time
}

// NewAttachWaitController constructs wait-for-sessions policy state.
func NewAttachWaitController(grace time.Duration) *AttachWaitController {
	return &AttachWaitController{grace: grace}
}

// AllowStart marks wait mode eligible after a disconnect.
func (c *AttachWaitController) AllowStart() {
	if c == nil {
		return
	}
	c.waitAllowed = true
}

// ClearAllowance clears wait-start allowance when sessions are available.
func (c *AttachWaitController) ClearAllowance() {
	if c == nil {
		return
	}
	c.waitAllowed = false
}

// CanStart reports whether wait mode should start when sessions are empty.
func (c *AttachWaitController) CanStart() bool {
	if c == nil {
		return false
	}
	return c.waitAllowed || c.waiting
}

// Start enters wait mode and returns true only on first transition.
func (c *AttachWaitController) Start(now time.Time) bool {
	if c == nil || c.waiting {
		return false
	}
	c.waiting = true
	c.waitAllowed = false
	c.waitUntil = now.Add(c.grace)
	return true
}

// Stop exits wait mode and returns whether wait mode was active.
func (c *AttachWaitController) Stop() bool {
	if c == nil {
		return false
	}
	wasWaiting := c.waiting
	c.waiting = false
	c.waitUntil = time.Time{}
	return wasWaiting
}

// Waiting reports whether wait mode is active.
func (c *AttachWaitController) Waiting() bool {
	if c == nil {
		return false
	}
	return c.waiting
}

// WaitUntil returns the wait deadline.
func (c *AttachWaitController) WaitUntil() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.waitUntil
}

// Expired reports whether wait mode deadline has elapsed.
func (c *AttachWaitController) Expired(now time.Time) bool {
	if c == nil || !c.waiting || c.waitUntil.IsZero() {
		return false
	}
	return !now.Before(c.waitUntil)
}
