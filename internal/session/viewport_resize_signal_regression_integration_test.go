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

func TestHostSIGWINCHPromptRedrawDoesNotCorruptPreservedWideScreen(t *testing.T) {
	shell := sigwinchPromptRedrawShell(t)

	master, slave, cmd, sess, waitErrCh := startSIGWINCHHelperHost(t, shell, 100, 12, nil)
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
		screen := sess.Screen()
		if !screen.Contains("RIGHT-11") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial fixed wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	sess.Resize(40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-11") {
		t.Fatalf("expected shrink to hide right edge on fixed wide screen, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	sess.Resize(100, 12)
	waitForRawContains(t, sess, "RIGHT-11", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored fixed wide content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("RIGHT-11") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected expand to restore fixed wide screen, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		lines := screen.Lines
		if len(lines) != len(baseline) {
			return fmt.Errorf("expected %d rows after prompt redraw, got %d\n%s", len(baseline), len(lines), screen.String())
		}
		for row := 0; row < len(baseline)-1; row++ {
			if lines[row] != baseline[row] {
				return fmt.Errorf("expected preserved row %d to remain stable after prompt redraw\nwant: %q\ngot:  %q\nscreen:\n%s", row+1, baseline[row], lines[row], screen.String())
			}
		}
		if !strings.HasPrefix(lines[len(lines)-1], "PROMPT> ") {
			return fmt.Errorf("expected prompt row after redraw, got %q\nscreen:\n%s", lines[len(lines)-1], screen.String())
		}
		if strings.Contains(lines[len(lines)-1], "RIGHT-11") {
			return fmt.Errorf("expected prompt row not to retain stale preserved content, got %q\nscreen:\n%s", lines[len(lines)-1], screen.String())
		}
		return nil
	})
}

func TestHostSIGWINCHPromptAdvanceDoesNotCorruptPreservedScrolledScreen(t *testing.T) {
	shell := sigwinchScrolledPromptShell(t)

	master, slave, cmd, sess, waitErrCh := startSIGWINCHHelperHost(t, shell, 60, 12, nil)
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
		screen := sess.Screen()
		if !screen.Contains("ROW-30") || !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial scrolled wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	sess.Resize(20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30-END") {
		t.Fatalf("expected shrink to hide right edge on scrolled wide screen, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	sess.Resize(60, 12)
	waitForRawContains(t, sess, "RIGHT-30-END", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored scrolled wide content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected expand to restore scrolled wide screen, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		lines := screen.Lines
		if len(lines) != len(baseline) {
			return fmt.Errorf("expected %d rows after prompt advance, got %d\n%s", len(baseline), len(lines), screen.String())
		}
		for row := 0; row < len(baseline)-2; row++ {
			want := baseline[row+1]
			if lines[row] != want {
				return fmt.Errorf("expected preserved row %d to advance cleanly after prompt\nwant: %q\ngot:  %q\nscreen:\n%s", row+1, want, lines[row], screen.String())
			}
		}
		if !strings.HasPrefix(lines[len(lines)-2], "PROMPT> ") {
			return fmt.Errorf("expected prompt row before trailing blank, got %q\nscreen:\n%s", lines[len(lines)-2], screen.String())
		}
		if strings.TrimSpace(lines[len(lines)-1]) != "" {
			return fmt.Errorf("expected trailing blank row after prompt advance, got %q\nscreen:\n%s", lines[len(lines)-1], screen.String())
		}
		return nil
	})
}

func TestHostSIGWINCHPsAuxAdvancePreservesExpandedScreen(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := sigwinchBashWrapper(t)

	master, slave, cmd, sess, waitErrCh := startSIGWINCHHelperHost(t, shell, 100, 12, []string{"PS1=PROMPT> "})
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
	sess.Send("clear; ps aux\n")
	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("PROMPT>") || !screen.Contains("bash") {
			return fmt.Errorf("expected ps aux output with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	sess.Resize(40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	_ = sess.DrainRaw()

	sess.Resize(100, 12)
	waitForRawContains(t, sess, "PROMPT>", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored ps aux screen after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected prompt after expand, got:\n%s", sess.Screen().String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		lines := screen.Lines
		if len(lines) != len(baseline) {
			return fmt.Errorf("expected %d rows after prompt advance, got %d\n%s", len(baseline), len(lines), screen.String())
		}
		matched := 0
		for row := 0; row < len(baseline)-1; row++ {
			if lines[row] == baseline[row] || lines[row] == baseline[min(row+1, len(baseline)-1)] {
				matched++
			}
		}
		if matched < len(baseline)-3 {
			return fmt.Errorf("expected ps aux screen to remain substantially preserved after expand and Enter; matched=%d/%d\nbefore:\n%s\nafter:\n%s", matched, len(baseline)-1, strings.Join(baseline, "\n"), screen.String())
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("expected prompt after Enter, got:\n%s", screen.String())
		}
		return nil
	})
}

func TestHostSIGWINCHTruncatedRedrawPreservesWideTails(t *testing.T) {
	shell := sigwinchTruncatedRedrawShell(t)

	master, slave, cmd, sess, waitErrCh := startSIGWINCHHelperHost(t, shell, 100, 12, nil)
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
		screen := sess.Screen()
		if !screen.Contains("ROW-11-LEFT") || !screen.Contains("RIGHT") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial fixed wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	sess.Resize(40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT") {
		t.Fatalf("expected shrink to hide preserved right tails, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	sess.Resize(100, 12)
	waitForRawContains(t, sess, "RIGHT", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored wide tails after SIGWINCH expand")
	_ = sess.DrainRaw()

	sess.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		lines := screen.Lines
		for row := 1; row < len(baseline)-1; row++ {
			if !strings.Contains(lines[row], "RIGHT") {
				return fmt.Errorf("expected preserved right tail on content row %d after truncated redraw, got %q\nscreen:\n%s", row+1, lines[row], screen.String())
			}
		}
		if !strings.HasPrefix(lines[len(lines)-1], "PROMPT> ") && !strings.HasPrefix(lines[len(lines)-2], "PROMPT> ") {
			return fmt.Errorf("expected prompt row after truncated redraw, got tail rows %q / %q\nscreen:\n%s", lines[len(lines)-2], lines[len(lines)-1], screen.String())
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

func sigwinchPromptRedrawShell(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sigwinch-prompt-redraw-shell.sh"
	const script = `#!/usr/bin/env bash
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

draw() {
  printf '\033[H\033[2J'
  for i in $(seq 1 11); do
    printf '\033[%d;1HROW-%02d-LEFT-1234567890-MID-abcdefghij-RIGHT-%02d' "$i" "$i" "$i"
  done
  printf '\033[12;1H\033[2KPROMPT> '
}

draw
while IFS= read -r -n1 ch; do
  case "$ch" in
    $'\n'|$'\r')
      printf '\033[12;1H\033[2KPROMPT> '
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write sigwinch prompt redraw shell: %v", err)
	}
	return path
}

func sigwinchScrolledPromptShell(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sigwinch-scrolled-prompt-shell.sh"
	const script = `#!/usr/bin/env bash
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

for i in $(seq 1 30); do
  printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-PAD-ABCDEFGHIJKLMNOPQRSTUVWXYZ-RIGHT-%02d-END\n' "$i" "$i"
done
printf 'PROMPT> '

while IFS= read -r -n1 ch; do
  case "$ch" in
    $'\n'|$'\r')
      printf '\r\nPROMPT> '
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write sigwinch scrolled prompt shell: %v", err)
	}
	return path
}

func sigwinchTruncatedRedrawShell(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sigwinch-truncated-redraw-shell.sh"
	const script = `#!/usr/bin/env bash
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

draw_full() {
  printf '\033[H\033[2J'
  for i in $(seq 1 11); do
    printf '\033[%d;1HROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-PAD-ABCDEFGHIJKLMNOPQRSTUVWXYZ-RIGHT-%02d-END' "$i" "$i" "$i"
  done
  printf '\033[12;1H\033[2KPROMPT> '
}

draw_truncated() {
  for i in $(seq 1 11); do
    printf -v line 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-PAD-ABCDEFGHIJKLMNOPQRSTUVWXYZ-RIGHT-%02d-END' "$i" "$i"
    printf '\033[%d;1H\033[2K%.40s' "$i" "$line"
  done
  printf '\033[12;1H\033[2KPROMPT> '
}

draw_full
while IFS= read -r -n1 ch; do
  case "$ch" in
    $'\n'|$'\r')
      draw_truncated
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write sigwinch truncated redraw shell: %v", err)
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
