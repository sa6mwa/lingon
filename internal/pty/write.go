package pty

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"pkt.systems/lingon/internal/clock"
)

// Writer delivers ordered input without allowing backpressure to hide cancellation.
type Writer interface {
	Write(context.Context, []byte) (int, error)
}

// NewWriter binds context-aware writes to a session-owned PTY. It does not own
// the file; the caller serializes writes and keeps it open for the session.
// A nil clock uses system time. The descriptor remains nonblocking.
func NewWriter(file *os.File, clk clock.Clock) Writer {
	return &fileWriter{file: file, clock: clk}
}

type fileWriter struct {
	file  *os.File
	clock clock.Clock
}

func (w *fileWriter) Write(ctx context.Context, data []byte) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if w.file == nil {
		return 0, os.ErrInvalid
	}
	raw, err := w.file.SyscallConn()
	if err != nil {
		return 0, err
	}
	var flagErr error
	if err := raw.Control(func(fd uintptr) { flagErr = syscall.SetNonblock(int(fd), true) }); err != nil {
		return 0, err
	}
	if flagErr != nil {
		return 0, flagErr
	}
	// Return EAGAIN to our cancellable retry loop rather than waiting inside
	// os.File.Write or RawConn.Write's internal poller.
	return writeAllWithRetry(ctx, func(ctx context.Context, delay time.Duration) error { return waitRetry(ctx, w.clock, delay) }, data, func(p []byte) (int, error) {
		var n int
		var writeErr error
		err := raw.Write(func(fd uintptr) bool { n, writeErr = syscall.Write(int(fd), p); return true })
		if err != nil {
			return 0, err
		}
		return max(n, 0), writeErr
	})
}

func waitRetry(ctx context.Context, clk clock.Clock, delay time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if clk == nil {
		clk = clock.New()
	}
	timer := clk.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func writeAllWithRetry(ctx context.Context, wait func(context.Context, time.Duration) error, data []byte, write func([]byte) (int, error)) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	written := 0
	for written < len(data) {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return written, err
			}
		}
		n, err := write(data[written:])
		if n > 0 {
			written += n
		}
		if err == nil {
			if n == 0 {
				return written, io.ErrShortWrite
			}
			continue
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			if ctx != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return written, ctxErr
				}
			}
			if wait != nil {
				if err := wait(ctx, 2*time.Millisecond); err != nil {
					return written, err
				}
			}
			continue
		}
		return written, err
	}
	return written, nil
}
