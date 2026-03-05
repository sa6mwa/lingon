//go:build !windows

package headless

import (
	"errors"
	"os"
	"syscall"
)

// PIDAlive reports whether a pid currently exists.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// TerminatePID sends SIGTERM to a pid.
func TerminatePID(pid int) error {
	if pid <= 0 {
		return os.ErrInvalid
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// KillPID sends SIGKILL to a pid.
func KillPID(pid int) error {
	if pid <= 0 {
		return os.ErrInvalid
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
