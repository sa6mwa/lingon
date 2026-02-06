//go:build linux

package session

import (
	"context"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func readPTY(ctx context.Context, file *os.File, buf []byte) (int, error) {
	if file == nil {
		return 0, io.EOF
	}
	fd := int(file.Fd())
	pollfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		if ctx != nil && ctx.Err() != nil {
			return 0, ctx.Err()
		}
		_, err := unix.Poll(pollfds, int(50*time.Millisecond/time.Millisecond))
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return 0, err
		}
		revents := pollfds[0].Revents
		if revents&(unix.POLLERR|unix.POLLHUP) != 0 {
			return file.Read(buf)
		}
		if revents&unix.POLLIN == 0 {
			continue
		}
		return file.Read(buf)
	}
}
