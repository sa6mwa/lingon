package session_test

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

// Fence the final context check after the private endpoint publishes the image.
// Returning the saved nil error lets cancellation arrive immediately after that
// check, before the writer can acquire the lock held by the test.
type publicationFence struct {
	context.Context
	checks          int
	reached, resume chan struct{}
}

func (c *publicationFence) Err() error {
	err := c.Context.Err()
	c.checks++
	if c.checks == 3 {
		close(c.reached)
		<-c.resume
	}
	return err
}

func TestClipboardCancelWhileWriterLocked(t *testing.T) {
	guard, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	endpoint, err := clipboard.NewEndpoint(guard, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	master, slave := ptytest.OpenPTY(t, 80, 24)
	state, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	terminal := ptytest.NewPTYSession(t, master, slave, 80, 24)
	defer func() {
		terminal.Close()
		_ = term.Restore(int(slave.Fd()), state)
		_ = slave.Close()
		_ = master.Close()
	}()
	input := session.NewClipboardInputFixture(guard, endpoint, session.ClipboardTerminal{Master: master, Slave: slave})
	unlock := input.HoldWriter()
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	ctx, cancel := context.WithCancel(guard)
	defer cancel()
	fence := &publicationFence{Context: ctx, reached: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan error, 1)
	img := clipboardPTYImage(t, "image/png")
	go func() { done <- input.Paste(fence, img) }()
	select {
	case <-fence.reached:
	case <-guard.Done():
		t.Fatal(guard.Err())
	}
	cancel()
	close(fence.resume)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled paste returned %v", err)
		}
	case <-guard.Done():
		t.Fatal("cancelled paste did not leave writer wait")
	}
	release()
	if _, err := master.Write([]byte("z")); err != nil {
		t.Fatal(err)
	}
	var key [1]byte
	if _, err := io.ReadFull(slave, key[:]); err != nil || key[0] != 'z' {
		t.Fatalf("cancelled paste delivered key %x, err=%v", key, err)
	}
}

type cancelRetryClock struct {
	clock.Clock
	retry func()
}

func (c cancelRetryClock) Sleep(time.Duration) { c.retry() }
func (c cancelRetryClock) NewTimer(d time.Duration) *clock.Timer {
	timer := c.Clock.NewTimer(d)
	c.retry()
	return timer
}

// This regression owns only a pipe; it never opens or modifies any terminal.
func TestClipboardCancelDuringWriteBackpressure(t *testing.T) {
	guard, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	endpoint, err := clipboard.NewEndpoint(guard, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	reader, writer := os.NewFile(uintptr(fds[0]), "cancel-read"), os.NewFile(uintptr(fds[1]), "cancel-write")
	defer reader.Close()
	defer writer.Close()
	raw, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	filled := 0
	if err := raw.Control(func(fd uintptr) {
		if err := unix.SetNonblock(int(fd), true); err != nil {
			t.Fatal(err)
		}
		for {
			n, err := unix.Write(int(fd), make([]byte, 4096))
			if n > 0 {
				filled += n
			}
			if errors.Is(err, unix.EAGAIN) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(guard)
	defer cancel()
	input := session.NewClipboardInputFixture(guard, endpoint, session.ClipboardTerminal{Master: writer})
	retries := 0
	input.SetClock(cancelRetryClock{Clock: clock.NewMock(), retry: func() {
		retries++
		cancel()
		if _, err := io.CopyN(io.Discard, reader, int64(filled)); err != nil {
			t.Fatal(err)
		}
	}})
	err = input.Paste(ctx, clipboardPTYImage(t, "image/png"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("backpressured cancelled paste returned %v", err)
	}
	if retries != 1 {
		t.Fatalf("retries=%d, want 1", retries)
	}
	if _, err := writer.Write([]byte("z")); err != nil {
		t.Fatal(err)
	}
	var key [1]byte
	if _, err := io.ReadFull(reader, key[:]); err != nil || key[0] != 'z' {
		t.Fatalf("cancelled paste delivered key %x, err=%v", key, err)
	}
}
