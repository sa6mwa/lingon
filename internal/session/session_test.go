package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRunnerLocalShellEcho(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	_ = pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24})

	r := New(Options{
		Shell:   "/bin/sh",
		Publish: false,
		Stdin:   slave,
		Stdout:  slave,
		Cols:    80,
		Rows:    24,
		DisableRaw: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx)
	}()

	if _, err := master.Write([]byte("printf 'READY\\n'\\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := readUntil(master, "READY", 2*time.Second); err != nil {
		t.Fatalf("readUntil: %v", err)
	}

	if _, err := master.Write([]byte("exit\\n")); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	_ = master.Close()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func readUntil(file *os.File, want string, timeout time.Duration) error {
	if err := syscall.SetNonblock(int(file.Fd()), true); err != nil {
		return err
	}
	defer func() {
		_ = syscall.SetNonblock(int(file.Fd()), false)
	}()

	var buf bytes.Buffer
	deadline := time.Now().Add(timeout)
	tmp := make([]byte, 1024)

	for time.Now().Before(deadline) {
		n, err := file.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if strings.Contains(buf.String(), want) {
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return fmt.Errorf("read error: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %q; got %q", want, buf.String())
}
