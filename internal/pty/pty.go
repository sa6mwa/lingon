package pty

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// Start launches the command attached to a new PTY.
func Start(cmd *exec.Cmd) (*os.File, error) {
	return pty.Start(cmd)
}

// Resize updates the PTY window size.
func Resize(ptyFile *os.File, cols, rows int) error {
	if ptyFile == nil {
		return nil
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return pty.Setsize(ptyFile, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
