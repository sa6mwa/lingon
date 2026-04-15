//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func disableTTYEcho(file *os.File) (func(), error) {
	termios, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err != nil {
		return nil, err
	}
	original := *termios
	termios.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, termios); err != nil {
		return nil, err
	}
	return func() {
		_ = unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &original)
	}, nil
}
