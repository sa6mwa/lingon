package session_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestHostBurstEnterKeepsConsecutivePromptNumbers(t *testing.T) {
	shell := countingPromptShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "prompt-burst-local",
		SessionName: "prompt-burst-local",
		Shell:       shell,
		Cols:        40,
		Rows:        8,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "prompt-burst-local", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptNumber(t, host, 1, 2*time.Second)

	for i := 0; i < 24; i++ {
		host.SendBytes([]byte{'\r'})
		host.Wait(50 * time.Millisecond)
	}

	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		rowNums := promptNumbersFromScreen(screen.String())
		if len(rowNums) != 7 {
			return fmt.Errorf("expected 7 numbered prompts in viewport, got %v\nscreen:\n%s", rowNums, screen.String())
		}
		wantStart := 19
		for i, got := range rowNums {
			want := wantStart + i
			if got != want {
				return fmt.Errorf("expected consecutive prompts %d..25, got %v\nscreen:\n%s", wantStart, rowNums, screen.String())
			}
		}
		cur := host.Cursor()
		if cur.Row != 8 {
			return fmt.Errorf("expected cursor on final prompt row 8, got %d\nscreen:\n%s", cur.Row, screen.String())
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte{0x1b, '[', 'H'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		rowNums := promptNumbersFromScreen(screen.String())
		if len(rowNums) < 7 {
			return fmt.Errorf("expected top scrollback page to show first prompts, got %v\nscreen:\n%s", rowNums, screen.String())
		}
		for i := 0; i < 7; i++ {
			want := i + 1
			if rowNums[i] != want {
				return fmt.Errorf("expected top scrollback prompts 1..7, got %v\nscreen:\n%s", rowNums, screen.String())
			}
		}
		return nil
	})
}

func TestHostBurstEnterKeepsConsecutiveBashPromptNumbers(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := ptytest.New(t)
	var ptyOut bytes.Buffer
	host := startHostWithPTYRead(t, h, session.Options{
		Endpoint:    h.Endpoint(),
		Token:       h.AccessToken(),
		AuthFile:    h.AuthFile(),
		SessionID:   "prompt-burst-bash",
		SessionName: "prompt-burst-bash",
		Shell:       countingPromptBash(t),
		Cols:        40,
		Rows:        8,
		Publish:     true,
		OnPTYRead: func(data []byte) {
			_, _ = ptyOut.Write(data)
		},
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "prompt-burst-bash", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)

	host.SendBytes([]byte(strings.Repeat("\n", 24)))

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		rowNums := promptNumbersFromScreen(screen.String())
		if len(rowNums) != 7 {
			return fmt.Errorf("expected 7 numbered bash prompts in viewport, got %v\npty=%q\nscreen:\n%s", rowNums, ptyOut.String(), screen.String())
		}
		wantStart := 19
		for i, got := range rowNums {
			want := wantStart + i
			if got != want {
				return fmt.Errorf("expected consecutive bash prompts %d..25, got %v\npty=%q\nscreen:\n%s", wantStart, rowNums, ptyOut.String(), screen.String())
			}
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte{0x1b, '[', 'H'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		rowNums := promptNumbersFromScreen(screen.String())
		if len(rowNums) < 7 {
			return fmt.Errorf("expected top scrollback page to show first bash prompts, got %v\nscreen:\n%s", rowNums, screen.String())
		}
		for i := 0; i < 7; i++ {
			want := i + 1
			if rowNums[i] != want {
				return fmt.Errorf("expected top scrollback bash prompts 1..7, got %v\nscreen:\n%s", rowNums, screen.String())
			}
		}
		return nil
	})
}

func waitForHostPromptNumber(t *testing.T, host *ptytest.PTYSession, want int, timeout time.Duration) {
	t.Helper()
	eventuallyWithClock(t, host.Clock(), timeout, 50*time.Millisecond, func() error {
		nums := promptNumbersFromScreen(host.Screen().String())
		if len(nums) == 0 {
			return fmt.Errorf("waiting for numbered prompt")
		}
		if nums[len(nums)-1] != want {
			return fmt.Errorf("waiting for prompt %d, got %v", want, nums)
		}
		return nil
	})
}

var promptNumberRe = regexp.MustCompile(`PROMPT-([0-9]{3})>`)

func promptNumbersFromScreen(screen string) []int {
	matches := promptNumberRe.FindAllStringSubmatch(screen, -1)
	out := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func countingPromptShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "counting-prompt-shell.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

count=1
line=''

draw_prompt() {
  printf 'PROMPT-%03d> ' "$count"
}

run_line() {
  printf '\r\n'
  line=''
  count=$((count+1))
  draw_prompt
}

draw_prompt
while IFS= read -rsn1 ch; do
  if [ -z "$ch" ]; then
    run_line
    continue
  fi
  case "$ch" in
    $'\r'|$'\n')
      run_line
      ;;
    *)
      line+="$ch"
      printf '%s' "$ch"
      ;;
  esac
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write counting prompt shell: %v", err)
	}
	return scriptPath
}

func countingPromptBash(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "bashrc")
	wrapperPath := filepath.Join(dir, "bash-wrapper.sh")
	const rc = `
count=0
update_prompt() {
  count=$((count+1))
  printf -v PS1 'PROMPT-%03d> ' "$count"
}
PROMPT_COMMAND=update_prompt
set +o emacs
set +o vi
`
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatalf("write bashrc: %v", err)
	}
	wrapper := fmt.Sprintf("#!/usr/bin/env bash\nexec /bin/bash --noprofile --rcfile %q -i\n", rcPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write bash wrapper: %v", err)
	}
	return wrapperPath
}

func startHostWithPTYRead(t *testing.T, h *ptytest.Harness, opts session.Options) *ptytest.PTYSession {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, opts.Cols, opts.Rows)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, opts.Cols, opts.Rows, opts.Clock)
	if opts.Stdin == nil {
		opts.Stdin = slave
	}
	if opts.Stdout == nil {
		opts.Stdout = slave
	}
	ctx, cancel := context.WithCancel(sess.Context())
	go func() {
		defer cancel()
		sess.SetRunErr(session.New(opts).Run(ctx))
	}()
	return sess
}
