package testpty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const ownedPTYEnv = "LINGON_TEST_OWN_PTY"

// MaybeReexecOwnedPTY re-execs the current test binary under an owned PTY so
// package tests never inherit the caller's controlling terminal for resize/raw
// operations. It returns handled=true in the parent process, along with the
// child exit code to propagate via os.Exit.
func MaybeReexecOwnedPTY() (handled bool, code int, err error) {
	if os.Getenv(ownedPTYEnv) == "1" {
		return false, 0, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) && !term.IsTerminal(int(os.Stdout.Fd())) {
		return false, 0, nil
	}
	cols, rows := currentTerminalSize()
	master, slave, err := pty.Open()
	if err != nil {
		return true, 1, fmt.Errorf("pty.Open: %w", err)
	}
	defer func() {
		_ = master.Close()
	}()
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		_ = slave.Close()
		return true, 1, fmt.Errorf("pty.Setsize: %w", err)
	}

	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), ownedPTYEnv+"=1")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return true, 1, fmt.Errorf("start test binary in pty: %w", err)
	}
	_ = slave.Close()

	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(os.Stdout, master)
		close(copyDone)
	}()

	waitErr := cmd.Wait()
	_ = master.Close()
	<-copyDone
	if waitErr == nil {
		return true, 0, nil
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return true, exitErr.ExitCode(), nil
	}
	return true, 1, waitErr
}

func currentTerminalSize() (cols, rows int) {
	for _, fd := range []int{int(os.Stdout.Fd()), int(os.Stdin.Fd())} {
		if c, r, err := term.GetSize(fd); err == nil && c > 0 && r > 0 {
			return c, r
		}
	}
	return 80, 24
}
