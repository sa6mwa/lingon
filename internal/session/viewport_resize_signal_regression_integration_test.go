package session_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	hostsession "pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput(t *testing.T) {
	shell := preservedWideScrollOutputShell(t)
	sess := startSIGWINCHInProcessHost(t, shell, 60, 12, nil)
	t.Cleanup(sess.Cancel)

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen().String()
		if !sess.Screen().Contains("RIGHT-30") {
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

	resizeProcessHost(t, sess, 20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge on signal-driven host, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 60, 12)
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
	sess := startSIGWINCHInProcessHost(t, shell, 60, 12, []string{"PS1=PROMPT> "})
	defer sess.Cancel()

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

	resizeProcessHost(t, sess, 20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge on signal-driven interactive host, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 60, 12)
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
	sess := startSIGWINCHInProcessHost(t, shell, 100, 12, nil)
	defer sess.Cancel()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("RIGHT-11") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial fixed wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-11") {
		t.Fatalf("expected shrink to hide right edge on fixed wide screen, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 100, 12)
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
	sess := startSIGWINCHInProcessHost(t, shell, 60, 12, nil)
	defer sess.Cancel()
	control := startSIGWINCHInProcessHost(t, shell, 60, 12, nil)
	defer control.Cancel()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("ROW-30") || !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial scrolled wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 20, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-30-END") {
		t.Fatalf("expected shrink to hide right edge on scrolled wide screen, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 60, 12)
	waitForRawContains(t, sess, "RIGHT-30-END", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored scrolled wide content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected expand to restore scrolled wide screen, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	eventuallyWithClock(t, control.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := control.Screen()
		if !screen.Contains("ROW-30") || !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected control scrolled wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})

	sess.Send("\r")
	control.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		return compareSessionScreens(sess, control, false)
	})
}

func TestHostSIGWINCHPromptAdvancePreservesExpandedMixedWidthScreen(t *testing.T) {
	shell := sigwinchMixedWidthPromptShell(t)
	sess := startSIGWINCHInProcessHost(t, shell, 100, 12, nil)
	defer sess.Cancel()
	control := startSIGWINCHInProcessHost(t, shell, 100, 12, nil)
	defer control.Cancel()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		screenText := screen.String()
		if !strings.Contains(screenText, "RIGHT-29-END") || !strings.Contains(screenText, "PROMPT>") {
			return fmt.Errorf("expected initial mixed-width scrolled screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT-29-END") {
		t.Fatalf("expected shrink to hide mixed-width right edge, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 100, 12)
	waitForRawContains(t, sess, "RIGHT-29-END", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored mixed-width content after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		screenText := screen.String()
		if !strings.Contains(screenText, "RIGHT-29-END") || !strings.Contains(screenText, "PROMPT>") {
			return fmt.Errorf("expected expand to restore mixed-width scrolled screen, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	eventuallyWithClock(t, control.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := control.Screen()
		screenText := screen.String()
		if !strings.Contains(screenText, "RIGHT-29-END") || !strings.Contains(screenText, "PROMPT>") {
			return fmt.Errorf("expected control mixed-width scrolled screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})

	sess.Send("\r")
	control.Send("\r")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		return compareSessionScreens(sess, control, false)
	})
}

func TestHostSIGWINCHPsAuxAdvancePreservesExpandedScreen(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := sigwinchBashWrapper(t)
	sess := startSIGWINCHInProcessHost(t, shell, 100, 12, []string{"PS1=PROMPT> "})
	defer sess.Cancel()
	control := startSIGWINCHInProcessHost(t, shell, 100, 12, []string{"PS1=PROMPT> "})
	defer control.Cancel()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected initial bash prompt from helper host, got:\n%s", sess.Screen().String())
		}
		return nil
	})
	sess.Send("clear; ps aux\n")
	control.Send("clear; ps aux\n")
	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("PROMPT>") || !screen.Contains("bash") {
			return fmt.Errorf("expected ps aux output with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	eventuallyWithClock(t, control.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := control.Screen()
		if !screen.Contains("PROMPT>") || !screen.Contains("bash") {
			return fmt.Errorf("expected control ps aux output with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 100, 12)
	waitForRawContains(t, sess, "PROMPT>", 4*time.Second, 50*time.Millisecond, "expected helper host to emit restored ps aux screen after SIGWINCH expand")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected prompt after expand, got:\n%s", sess.Screen().String())
		}
		return nil
	})
	_ = sess.DrainRaw()

	sess.Send("\r")
	control.Send("\r")
	waitForRawChunkContains(t, sess, "PROMPT>", 2*time.Second, 50*time.Millisecond, "expected prompt redraw after Enter")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		return compareSessionScreens(sess, control, true)
	})
}

func TestHostSIGWINCHTruncatedRedrawPreservesWideTails(t *testing.T) {
	shell := sigwinchTruncatedRedrawShell(t)
	sess := startSIGWINCHInProcessHost(t, shell, 100, 12, nil)
	defer sess.Cancel()

	eventuallyWithClock(t, sess.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := sess.Screen()
		if !screen.Contains("ROW-11-LEFT") || !screen.Contains("RIGHT") || !screen.Contains("PROMPT> ") {
			return fmt.Errorf("expected initial fixed wide screen with prompt, got:\n%s", screen.String())
		}
		return nil
	})
	baseline := append([]string(nil), sess.Screen().Lines...)
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 40, 6)
	waitForRawIdle(t, sess, 150*time.Millisecond, 3*time.Second)
	if sess.Screen().Contains("RIGHT") {
		t.Fatalf("expected shrink to hide preserved right tails, got:\n%s", sess.Screen().String())
	}
	_ = sess.DrainRaw()

	resizeProcessHost(t, sess, 100, 12)
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

func startSIGWINCHInProcessHost(t *testing.T, shell string, cols, rows int, extraEnv []string) *ptytest.PTYSession {
	t.Helper()
	for _, entry := range extraEnv {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid env override %q", entry)
		}
		t.Setenv(parts[0], parts[1])
	}
	return startHostWithPTYRead(t, nil, hostsession.Options{
		SessionID:   "sigwinch-helper",
		SessionName: "sigwinch-helper",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
		Publish:     false,
	})
}

func resizeProcessHost(t *testing.T, sess *ptytest.PTYSession, cols, rows int) {
	t.Helper()
	sess.Resize(cols, rows)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("signal process SIGWINCH: %v", err)
	}
}

var dynamicNumberRe = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

func compareSessionScreens(got, want *ptytest.PTYSession, normalizeNumbers bool) error {
	gotLines := append([]string(nil), got.Screen().Lines...)
	wantLines := append([]string(nil), want.Screen().Lines...)
	if len(gotLines) != len(wantLines) {
		return fmt.Errorf("screen row count mismatch: got %d want %d\ngot:\n%s\nwant:\n%s", len(gotLines), len(wantLines), got.Screen().String(), want.Screen().String())
	}
	for i := range gotLines {
		gotLine := gotLines[i]
		wantLine := wantLines[i]
		if normalizeNumbers {
			gotLine = normalizeDynamicScreenLine(gotLine)
			wantLine = normalizeDynamicScreenLine(wantLine)
		}
		if gotLine != wantLine {
			return fmt.Errorf("screen row %d mismatch\ngot:  %q\nwant: %q\nfull got:\n%s\nfull want:\n%s", i+1, gotLine, wantLine, got.Screen().String(), want.Screen().String())
		}
	}
	return nil
}

func normalizeDynamicScreenLine(line string) string {
	return dynamicNumberRe.ReplaceAllString(line, "#")
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

func sigwinchMixedWidthPromptShell(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sigwinch-mixed-width-prompt-shell.sh"
	const script = `#!/usr/bin/env bash
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

for i in $(seq 1 30); do
  if (( i % 3 == 0 )); then
    printf 'SHORT-%02d /var/tmp/item-%02d\n' "$i" "$i"
  elif (( i % 3 == 1 )); then
    printf 'MID-%02d /opt/example/component-%02d with moderate text width\n' "$i" "$i"
  else
    printf 'WIDE-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\n' "$i" "$i"
  fi
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
		t.Fatalf("write sigwinch mixed-width prompt shell: %v", err)
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
