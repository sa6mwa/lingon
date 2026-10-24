//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

// withTTYDescriptor retains the descriptor throughout the ioctl operation, including
// a read-modify-write pair. Concurrent Close cannot destroy or reuse it.
func withTTYDescriptor(file *os.File, fn func(int) error) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := raw.Control(func(fd uintptr) {
		ioctlErr = fn(int(fd))
	}); err != nil {
		return err
	}
	return ioctlErr
}

func readTermios(file *os.File) (unix.Termios, error) {
	var value unix.Termios
	err := withTTYDescriptor(file, func(fd int) error {
		termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err == nil {
			value = *termios
		}
		return err
	})
	return value, err
}
