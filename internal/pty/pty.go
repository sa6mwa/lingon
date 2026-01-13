package pty

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// Start launches the command attached to a new PTY.
func Start(cmd *exec.Cmd) (*os.File, error) {
	return pty.Start(cmd)
}

// StartWithTTY launches the command attached to a new PTY and returns both master and slave.
func StartWithTTY(cmd *exec.Cmd) (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if cmd.Stdin == nil {
		cmd.Stdin = slave
	}
	if cmd.Stdout == nil {
		cmd.Stdout = slave
	}
	if cmd.Stderr == nil {
		cmd.Stderr = slave
	}
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
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
