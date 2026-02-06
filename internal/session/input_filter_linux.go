//go:build linux

package session

import (
	"os"

	"golang.org/x/sys/unix"
)

func filterRemoteInput(ttyFile *os.File, data []byte) []byte {
	if ttyFile == nil || len(data) == 0 {
		return data
	}
	termios, err := unix.IoctlGetTermios(int(ttyFile.Fd()), unix.TCGETS)
	if err != nil {
		return data
	}
	if termios.Lflag&unix.ICANON == 0 {
		return data
	}
	veof := termios.Cc[unix.VEOF]
	if veof != 0x04 {
		return data
	}
	// Filter Ctrl-D in canonical mode to avoid EOF from remote control.
	out := data[:0]
	for _, b := range data {
		if b == 0x04 {
			continue
		}
		out = append(out, b)
	}
	return out
}
