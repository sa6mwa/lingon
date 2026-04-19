package session_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	hostsession "pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput(t *testing.T) {
	shell := preservedWideScrollOutputShell(t)

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 60, Rows: 12}); err != nil {
		t.Fatalf("pty.Setsize initial: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHostSIGWINCHHelperProcess", "--")
	cmd.Env = append(os.Environ(),
		"LINGON_SIGWINCH_HELPER=1",
		"LINGON_SIGWINCH_SHELL="+shell,
		"LINGON_SIGWINCH_COLS=60",
		"LINGON_SIGWINCH_ROWS=12",
	)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper host: %v", err)
	}
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
	}()

	sess := ptytest.NewPTYSession(t, master, slave, 60, 12)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-waitErrCh:
		default:
		}
		sess.Cancel()
	})

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen().String()
		if !sess.Screen().Contains("RIGHT-30") {
			select {
			case err := <-waitErrCh:
				return fmt.Errorf("helper exited err=%v before initial wide output:\n%s", err, screen)
			default:
			}
			return fmt.Errorf("waiting for initial scrolled wide output:\n%s", screen)
		}
		return nil
	})
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected initial prompt from helper host, got:\n%s", sess.Screen().String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Resize(20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge on signal-driven host, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	sess.Resize(60, 12)
	waitForRawContains(t, sess, "RIGHT-30", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored wide content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen().String()
		if !sess.Screen().Contains("RIGHT-30") || !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected expand to restore scrolled wide output after real SIGWINCH, got:\n%s", screen)
		}
		return nil
	})
}

func TestHostSIGWINCHPreservesInteractiveWideOutputWithoutInput(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := sigwinchBashWrapper(t)

	master, slave, cmd, sess, waitErrCh := startSIGWINCHHelperHost(t, shell, 60, 12, []string{"PS1=PROMPT> "})
	defer func() {
		_ = master.Close()
		_ = slave.Close()
		_ = cmd.Process.Kill()
		select {
		case <-waitErrCh:
		default:
		}
		sess.Cancel()
	}()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected initial bash prompt from helper host, got:\n%s", sess.Screen().String())
		}
		return nil
	})
	sess.Send("clear; for i in $(seq 1 30); do printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghij-RIGHT-%02d\\n' \"$i\" \"$i\"; done\n")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen().String()
		if !sess.Screen().Contains("RIGHT-30") || !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected initial interactive wide output with prompt, got:\n%s", screen)
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Resize(20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge on signal-driven interactive host, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	sess.Resize(60, 12)
	expandRaw := waitForRawChunkContains(t, sess, "RIGHT-30", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored interactive wide content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen().String()
		if !sess.Screen().Contains("RIGHT-30") || !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected expand to restore interactive wide output after real SIGWINCH, got:\n%s\nraw:\n%q", screen, expandRaw)
		}
		return nil
	})
}

func TestHostSIGWINCHHelperProcess(t *testing.T) {
	if os.Getenv("LINGON_SIGWINCH_HELPER") != "1" {
		t.Skip("helper process only")
	}

	shell := os.Getenv("LINGON_SIGWINCH_SHELL")
	if shell == "" {
		t.Fatal("missing LINGON_SIGWINCH_SHELL")
	}
	cols, err := strconv.Atoi(os.Getenv("LINGON_SIGWINCH_COLS"))
	if err != nil || cols <= 0 {
		t.Fatalf("invalid helper cols: %v", err)
	}
	rows, err := strconv.Atoi(os.Getenv("LINGON_SIGWINCH_ROWS"))
	if err != nil || rows <= 0 {
		t.Fatalf("invalid helper rows: %v", err)
	}

	runner := hostsession.New(hostsession.Options{
		SessionID:   "sigwinch-helper",
		SessionName: "sigwinch-helper",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
		Publish:     false,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		DisableRaw:  true,
	})

	if err := runner.Run(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("helper host run: %v", err)
	}
}

func startSIGWINCHHelperHost(t *testing.T, shell string, cols, rows int, extraEnv []string) (*os.File, *os.File, *exec.Cmd, *ptytest.PTYSession, <-chan error) {
	t.Helper()

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		t.Fatalf("pty.Setsize initial: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHostSIGWINCHHelperProcess", "--")
	cmd.Env = append(os.Environ(),
		"LINGON_SIGWINCH_HELPER=1",
		"LINGON_SIGWINCH_SHELL="+shell,
		"LINGON_SIGWINCH_COLS="+strconv.Itoa(cols),
		"LINGON_SIGWINCH_ROWS="+strconv.Itoa(rows),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper host: %v", err)
	}
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
	}()

	sess := ptytest.NewPTYSession(t, master, slave, cols, rows)
	return master, slave, cmd, sess, waitErrCh
}

func sigwinchBashWrapper(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sigwinch-bash-wrapper.sh"
	const script = `#!/usr/bin/env bash
export PS1='PROMPT> '
exec /bin/bash --noprofile --norc -i
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write sigwinch bash wrapper: %v", err)
	}
	return path
}

func waitForRawChunkContains(t *testing.T, sess *ptytest.PTYSession, substr string, timeout, step time.Duration, msg string) string {
	t.Helper()
	deadline := sess.Clock().Now().Add(timeout)
	var seen strings.Builder
	for sess.Clock().Now().Before(deadline) {
		chunk := sess.DrainRaw()
		seen.WriteString(chunk)
		if strings.Contains(seen.String(), substr) {
			return seen.String()
		}
		advanceTestClock(sess.Clock(), step)
	}
	t.Fatalf("%s", msg)
	return ""
}
