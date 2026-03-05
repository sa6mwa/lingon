//go:build windows

package headless

import "os"

// PIDAlive reports whether a pid currently exists.
func PIDAlive(pid int) bool {
	return pid > 0
}

// TerminatePID sends a termination signal to a pid.
func TerminatePID(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// KillPID force-kills a pid.
func KillPID(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
