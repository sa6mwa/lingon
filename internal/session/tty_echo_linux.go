//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func disableTTYEcho(file *os.File) (func(), error) {
	var original unix.Termios
	changed := false
	err := withTTYDescriptor(file, func(fd int) error {
		termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			return err
		}
		if termios.Lflag&unix.ECHO == 0 {
			return nil
		}
		original = *termios
		termios.Lflag &^= unix.ECHO
		changed = true
		return unix.IoctlSetTermios(fd, unix.TCSETS, termios)
	})
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	return func() {
		_ = withTTYDescriptor(file, func(fd int) error {
			return unix.IoctlSetTermios(fd, unix.TCSETS, &original)
		})
	}, nil
}
