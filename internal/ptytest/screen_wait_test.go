package ptytest

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestWaitForScreenObservesOutputWithoutAdvancingClock(t *testing.T) {
	master, slave := OpenPTY(t, 40, 10)
	clk := clock.NewMock()
	s := NewPTYSessionWithClock(t, master, slave, 40, 10, clk)
	s.cleanup = func() { _ = master.Close(); _ = slave.Close() }
	initial := clk.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	checked := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- s.WaitForScreen(ctx, func(screen Screen) bool {
			once.Do(func() { close(checked) })
			return screen.Contains("OUTPUT-ACK")
		})
	}()
	select {
	case <-checked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := slave.Write([]byte("OUTPUT-ACK\n")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !clk.Now().Equal(initial) {
		t.Fatal("waiting for output advanced the inactivity clock")
	}
	// Already-rendered output must also work without needing a new read.
	if err := s.WaitForScreen(ctx, func(screen Screen) bool { return screen.Contains("OUTPUT-ACK") }); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForScreenStopsOnCancellationOrClosedPTY(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "closed"}[closed], func(t *testing.T) {
			master, slave := OpenPTY(t, 40, 10)
			s := NewPTYSession(t, master, slave, 40, 10)
			s.cleanup = func() { _ = master.Close(); _ = slave.Close() }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.Canceled
			if closed {
				s.Cancel()
				want = io.EOF
			} else {
				cancel()
			}
			if err := s.WaitForScreen(ctx, func(Screen) bool { return false }); !errors.Is(err, want) {
				t.Fatalf("WaitForScreen = %v, want %v", err, want)
			}
		})
	}
}
