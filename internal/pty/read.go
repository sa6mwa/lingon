package pty

import (
	"context"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Read reads a session-owned PTY without blocking cancellation or closure.
func Read(ctx context.Context, file *os.File, buf []byte) (int, error) {
	if file == nil {
		return 0, io.EOF
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, err
	}
	pollfds := []unix.PollFd{{Events: unix.POLLIN}}
	for {
		if ctx != nil && ctx.Err() != nil {
			return 0, ctx.Err()
		}
		var n int
		var ready bool
		var readErr error
		// Protect the descriptor throughout each bounded poll/read attempt.
		// Return after the poll even without input so cancellation and closure
		// are observed without retaining a possibly reused numeric descriptor.
		err := raw.Read(func(fd uintptr) bool {
			pollfds[0].Fd = int32(fd)
			_, readErr = unix.Poll(pollfds, int(50*time.Millisecond/time.Millisecond))
			if readErr == nil && pollfds[0].Revents&(unix.POLLIN|unix.POLLERR|unix.POLLHUP) != 0 {
				ready = true
				n, readErr = unix.Read(int(fd), buf)
			}
			return true
		})
		if err != nil {
			return 0, err
		}
		if readErr == syscall.EINTR || readErr == syscall.EAGAIN || readErr == syscall.EWOULDBLOCK {
			continue
		}
		if readErr != nil {
			return 0, readErr
		}
		if ready {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}
	}
}
