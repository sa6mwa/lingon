package pty

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestWriteAllWithRetryHandlesPartialWrite(t *testing.T) {
	calls := 0
	write := func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return 2, nil
		}
		return len(p), nil
	}

	n, err := writeAllWithRetry(context.Background(), nil, []byte("abcd"), write)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("written=%d, want 4", n)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestWriteAllWithRetryRetriesWouldBlock(t *testing.T) {
	calls := 0
	sleepCalls := 0
	write := func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return 0, syscall.EAGAIN
		}
		return len(p), nil
	}
	sleep := func(_ context.Context, _ time.Duration) error {
		sleepCalls++
		return nil
	}

	n, err := writeAllWithRetry(context.Background(), sleep, []byte("ok"), write)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Fatalf("written=%d, want 2", n)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if sleepCalls != 1 {
		t.Fatalf("sleepCalls=%d, want 1", sleepCalls)
	}
}

func TestWriteAllWithRetryReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	write := func([]byte) (int, error) {
		calls++
		return 0, syscall.EAGAIN
	}

	n, err := writeAllWithRetry(ctx, nil, []byte("x"), write)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context canceled", err)
	}
	if n != 0 {
		t.Fatalf("written=%d, want 0", n)
	}
	if calls != 0 {
		t.Fatalf("calls=%d, want 0", calls)
	}
}

func TestWriteAllWithRetryReturnsShortWrite(t *testing.T) {
	write := func([]byte) (int, error) {
		return 0, nil
	}

	n, err := writeAllWithRetry(context.Background(), nil, []byte("x"), write)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("err=%v, want io.ErrShortWrite", err)
	}
	if n != 0 {
		t.Fatalf("written=%d, want 0", n)
	}
}

type retryTimerClock struct {
	clock.Clock
	registered chan struct{}
}

func (c retryTimerClock) NewTimer(d time.Duration) *clock.Timer {
	timer := c.Clock.NewTimer(d)
	close(c.registered)
	return timer
}

func TestPTYRetryUsesClockAndCancellation(t *testing.T) {
	for _, cancelWait := range []bool{false, true} {
		name := "advance"
		if cancelWait {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			guard, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			mock := clock.NewMock()
			c := retryTimerClock{Clock: mock, registered: make(chan struct{})}
			ctx, cancel := context.WithCancel(guard)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- waitRetry(ctx, c, 2*time.Millisecond) }()
			select {
			case <-c.registered:
			case <-guard.Done():
				t.Fatal("retry timer was not registered")
			}
			if cancelWait {
				cancel() // Cancellation must wake the retry without advancing time.
			} else {
				mock.Add(2 * time.Millisecond)
			}
			select {
			case err := <-done:
				if cancelWait && !errors.Is(err, context.Canceled) || !cancelWait && err != nil {
					t.Fatalf("retry returned %v", err)
				}
			case <-guard.Done():
				t.Fatal("retry did not wake")
			}
		})
	}
}
