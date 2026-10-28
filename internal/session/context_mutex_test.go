package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// These tests exercise synchronization only; no terminal or session is started.
func TestContextMutexCancellation(t *testing.T) {
	guard, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	var mu contextMutex
	if err := mu.Lock(guard); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(guard)
	done := make(chan error, 1)
	go func() { done <- mu.Lock(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting lock returned %v", err)
		}
	case <-guard.Done():
		t.Fatal("cancelled waiter did not return while lock was held")
	}
	mu.Unlock()
	if err := mu.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("uncontended cancelled lock returned %v", err)
	}
	if err := mu.Lock(guard); err != nil {
		t.Fatal("cancelled waiter retained lock", err)
	}
	mu.Unlock()
}
