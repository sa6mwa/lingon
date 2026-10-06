package testclock

import (
	"testing"
	"time"
)

func TestAdvanceCompletesCallbacksAndPreservesStopReset(t *testing.T) {
	c := New()
	count := 0
	timer := c.AfterFunc(2*time.Second, func() { count++ })
	c.Add(time.Second)
	if count != 0 {
		t.Fatal("callback fired early")
	}
	timer.Reset(2 * time.Second)
	c.Add(time.Second)
	if count != 0 {
		t.Fatal("reset lost its new deadline")
	}
	c.Add(time.Second)
	if count != 1 {
		t.Fatalf("Add returned before callback completion: %d", count)
	}
	c.AfterFunc(time.Second, func() { count += 10 }).Stop()
	c.Add(time.Second)
	if count != 1 {
		t.Fatal("stopped callback fired")
	}
}
