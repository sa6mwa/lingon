//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func getVEOF(file *os.File) (uint8, error) {
	termios, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err != nil {
		return 0, err
	}
	return termios.Cc[unix.VEOF], nil
}

func setVEOF(file *os.File, value uint8) error {
	termios, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err != nil {
		return err
	}
	termios.Cc[unix.VEOF] = value
	return unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, termios)
}
