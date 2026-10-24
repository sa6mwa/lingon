package testclock

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentRegistrationAndAdvancement(t *testing.T) {
	c := New()
	var fired atomic.Int64
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 4 {
		workers.Go(func() {
			<-start
			for range 100 {
				c.AfterFunc(time.Hour, func() { fired.Add(1) })
			}
		})
	}
	workers.Go(func() {
		<-start
		for range 100 {
			c.Add(0)
		}
	})
	close(start)
	workers.Wait()
	c.Add(time.Hour)
	if got := fired.Load(); got != 400 {
		t.Fatalf("registered callbacks completed = %d, want 400", got)
	}
}

func TestAdvanceJoinsCallbacksScheduledByCallbacks(t *testing.T) {
	c := New()
	count := 0
	c.AfterFunc(time.Second, func() {
		count++
		c.AfterFunc(0, func() { count++ })
	})
	c.Add(time.Second)
	if count != 2 {
		t.Fatalf("Add returned before nested callbacks completed: %d", count)
	}
}

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
