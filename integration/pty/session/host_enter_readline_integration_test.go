//go:build integration

package integrationptysession_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

func TestHostRepeatedEnterWithReadlineKeepsEveryPromptRow(t *testing.T) {
	for _, bracketed := range []bool{false, true} {
		t.Run(fmt.Sprintf("bracketed-paste=%t", bracketed), func(t *testing.T) {
			shell := readlineCountingPromptBash(t, bracketed)
			h := newHarness(t)
			var raw testutil.OutputBuffer
			host := h.StartHost(ptytest.HostOptions{
				SessionID: "enter-readline", Shell: shell, Cols: 100, Rows: 40,
				OnPTYRead: func(p []byte) { _, _ = raw.Write(p) },
			})
			t.Cleanup(host.Cancel)
			waitScreen := func(check func(ptytest.Screen) bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				before := h.Clock().Now()
				if err := host.WaitForScreen(ctx, check); err != nil {
					t.Fatalf("waiting for prompt: %v\nchild PTY: %q\nhost:\n%s", err, raw.String(), host.Screen().String())
				}
				if !h.Clock().Now().Equal(before) {
					t.Fatal("prompt observation advanced application time")
				}
			}
			waitScreen(func(s ptytest.Screen) bool { return s.Contains("PROMPT-001>") })
			last := 1
			for phase, burst := range []int{128, 256, 128, 128} {
				if phase == 3 {
					for range burst {
						host.Send("\r")
					}
				} else {
					host.Send(strings.Repeat("\r", burst))
				}
				last += burst
				waitScreen(func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("PROMPT-%03d>", last)) })
				screen := host.Screen()
				for row := 1; row < 40; row++ {
					want := fmt.Sprintf("PROMPT-%03d>", last-39+row)
					if !strings.Contains(screen.Row(row), want) {
						t.Fatalf("row %d: want %s, got %q\nchild PTY: %q\nhost:\n%s", row+1, want, screen.Row(row), raw.String(), screen.String())
					}
				}
			}
			host.Send("printf 'AFTER_%s\\n' BURST\r")
			waitScreen(func(s ptytest.Screen) bool {
				return s.Contains("AFTER_BURST") && s.Contains(fmt.Sprintf("PROMPT-%03d>", last+1))
			})
		})
	}
}

// readlineCountingPromptBash uses ordinary interactive Bash, including its
// editing and terminal-mode transitions. Canonical input echo without Readline
// queues blank lines ahead of prompts even on a bare PTY.
func readlineCountingPromptBash(t *testing.T, bracketed bool) string {
	t.Helper()
	dir := t.TempDir()
	rc := filepath.Join(dir, "bashrc")
	setting := "off"
	if bracketed {
		setting = "on"
	}
	script := fmt.Sprintf(`HISTFILE=
count=0
bind 'set enable-bracketed-paste %s'
update_prompt() {
 count=$((count+1))
 printf -v PS1 '\[\e[32m\]PROMPT-%%03d> \[\e[0m\]' "$count"
}
PROMPT_COMMAND=update_prompt
printf '\n'
`, setting)
	if err := os.WriteFile(rc, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(dir, "shell")
	quotedRC := "'" + strings.ReplaceAll(rc, "'", "'\\''") + "'"
	if err := os.WriteFile(shell, []byte(fmt.Sprintf("#!/bin/sh\nexec /bin/bash --noprofile --rcfile %s -i\n", quotedRC)), 0700); err != nil {
		t.Fatal(err)
	}
	return shell
}
