package relayhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"pkt.systems/lingon/internal/pty"
)

type inputCancellationFence struct {
	context.Context
	cancel  context.CancelFunc
	release func()
}

func (c inputCancellationFence) Err() error {
	err := c.Context.Err()
	if err == nil {
		// Cancel after the caller has observed a successful context check,
		// then release the full pipe. A stale blocking write must not follow.
		c.cancel()
		c.release()
	}
	return err
}

// Only private pipe descriptors are used; no terminal is accessed.
func TestSDKWriteCancellationAfterContextCheck(t *testing.T) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	r, w := os.NewFile(uintptr(fds[0]), "sdk-cancel-read"), os.NewFile(uintptr(fds[1]), "sdk-cancel-write")
	defer r.Close()
	defer w.Close()
	raw, err := w.SyscallConn()
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fence := inputCancellationFence{Context: ctx, cancel: cancel, release: func() {
		if _, err := io.CopyN(io.Discard, r, int64(filled)); err != nil {
			t.Fatal(err)
		}
	}}
	if err := writeInput(fence, pty.NewWriter(w, nil), []byte{0x16}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled SDK write returned %v", err)
	}
	if _, err := w.Write([]byte("z")); err != nil {
		t.Fatal(err)
	}
	var key [1]byte
	if _, err := io.ReadFull(r, key[:]); err != nil || key[0] != 'z' {
		t.Fatalf("cancelled SDK write delivered %x before next input, err=%v", key, err)
	}
}

type inputWriterFunc func([]byte) (int, error)

func (f inputWriterFunc) Write(_ context.Context, data []byte) (int, error) { return f(data) }

func TestWriteInputCompletesPartialWrites(t *testing.T) {
	var got bytes.Buffer
	writer := inputWriterFunc(func(data []byte) (int, error) { return got.Write(data[:min(2, len(data))]) })
	if err := writeInput(context.Background(), writer, []byte("ordinary input\x04")); err != nil {
		t.Fatal(err)
	}
	if got.String() != "ordinary input\x04" {
		t.Fatal("partial writes lost or reordered keyboard/EOF bytes")
	}
}

func TestEnqueueInputBackpressureInvalidatesPaste(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := make(chan inputOperation, 1)
	queue <- inputOperation{data: []byte("earlier")}
	reset := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- enqueueInput(ctx, []byte("later"), queue, func() { close(reset) }) }()
	select {
	case <-reset:
	case <-ctx.Done():
		t.Fatal("reader stalled before invalidating pending paste")
	}
	if string((<-queue).data) != "earlier" {
		t.Fatal("backpressure lost earlier input")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("input did not resume")
	}
	if string((<-queue).data) != "later" {
		t.Fatal("backpressure reordered input")
	}
}

func TestWriteInputRejectsWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer inputWriterFunc
		want   error
	}{
		{"write error", func([]byte) (int, error) { return 0, io.ErrClosedPipe }, io.ErrClosedPipe},
		{"zero progress", func([]byte) (int, error) { return 0, nil }, io.ErrShortWrite},
		{"invalid count", func([]byte) (int, error) { return -1, nil }, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := writeInput(context.Background(), tc.writer, []byte{0x16}); !errors.Is(err, tc.want) {
				t.Fatalf("incorrect write result: %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWriteInputCancellationStopsRemainingBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got bytes.Buffer
	writer := inputWriterFunc(func(data []byte) (int, error) {
		cancel()
		return got.Write(data[:1])
	})
	if err := writeInput(ctx, writer, []byte("ab")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled write acknowledged remaining bytes", err)
	}
	if got.String() != "a" {
		t.Fatal("cancelled input continued writing")
	}
}
