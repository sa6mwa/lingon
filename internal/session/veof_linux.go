//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func getVEOF(file *os.File) (uint8, error) {
	termios, err := readTermios(file)
	return termios.Cc[unix.VEOF], err
}

func setVEOF(file *os.File, value uint8) error {
	return withTTYDescriptor(file, func(fd int) error {
		termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			return err
		}
		if termios.Cc[unix.VEOF] == value {
			return nil
		}
		termios.Cc[unix.VEOF] = value
		return unix.IoctlSetTermios(fd, unix.TCSETS, termios)
	})
}
