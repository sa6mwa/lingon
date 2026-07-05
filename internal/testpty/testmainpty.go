package testpty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const ownedPTYEnv = "LINGON_TEST_OWN_PTY"
const maxChildOutputBytes = 1 << 20

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
	childOutput := newBoundedTailBuffer(maxChildOutputBytes)
	go func() {
		_, _ = io.Copy(childOutput, master)
		close(copyDone)
	}()

	waitErr := cmd.Wait()
	_ = master.Close()
	<-copyDone
	if waitErr == nil {
		return true, 0, nil
	}
	writeSanitizedChildOutput(os.Stdout, childOutput.Bytes())
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return true, exitErr.ExitCode(), nil
	}
	return true, 1, waitErr
}

type boundedTailBuffer struct {
	limit int
	buf   []byte
}

func newBoundedTailBuffer(limit int) *boundedTailBuffer {
	if limit <= 0 {
		limit = 1
	}
	return &boundedTailBuffer{limit: limit}
}

func (b *boundedTailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		return written, nil
	}
	if len(b.buf)+len(p) > b.limit {
		drop := len(b.buf) + len(p) - b.limit
		copy(b.buf, b.buf[drop:])
		b.buf = b.buf[:len(b.buf)-drop]
	}
	b.buf = append(b.buf, p...)
	return written, nil
}

func (b *boundedTailBuffer) Bytes() []byte {
	return append([]byte(nil), b.buf...)
}

func writeSanitizedChildOutput(w io.Writer, data []byte) {
	if len(data) == 0 {
		return
	}
	_, _ = fmt.Fprint(w, sanitizeChildOutput(data))
}

func sanitizeChildOutput(data []byte) string {
	var out bytes.Buffer
	for _, b := range data {
		switch b {
		case '\n', '\t':
			out.WriteByte(b)
		default:
			if b >= 0x20 && b < 0x7f {
				out.WriteByte(b)
				continue
			}
			fmt.Fprintf(&out, "\\x%02x", b)
		}
	}
	return out.String()
}

func currentTerminalSize() (cols, rows int) {
	for _, fd := range []int{int(os.Stdout.Fd()), int(os.Stdin.Fd())} {
		if c, r, err := term.GetSize(fd); err == nil && c > 0 && r > 0 {
			return c, r
		}
	}
	return 80, 24
}
