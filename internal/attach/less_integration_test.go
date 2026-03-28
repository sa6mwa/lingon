package attach_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

var lessVisibleLinePattern = regexp.MustCompile(`LESS-LINE-(\d{3})`)

func TestAttachLessArrowKeysFollowApplicationCursorMode(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less not installed")
	}

	h := newHarness(t)
	sessionID := "attach-less-single"
	h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       lessTestShell(t),
		Cols:        100,
		Rows:        20,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:      sessionID,
		RequestControl: true,
		Cols:           100,
		Rows:           20,
	})
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	waitForAttachLessTopLine(t, h, attach, 3*time.Second, func(line int) bool { return line == 1 })

	attach.SendBytes([]byte("j"))
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 2 })

	attach.SendBytes([]byte{0x1b, '[', 'B'})
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 3 })

	attach.SendBytes([]byte{0x1b, '[', 'A'})
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 2 })
}

func TestMultiAttachLessArrowKeysFollowApplicationCursorMode(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less not installed")
	}

	h := newHarness(t)
	sessionID := "attach-less-multi"
	h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       lessTestShell(t),
		Cols:        100,
		Rows:        20,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      100,
		Rows:      20,
	})
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	waitForAttachLessTopLine(t, h, attach, 3*time.Second, func(line int) bool { return line == 2 })

	attach.SendBytes([]byte("j"))
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 3 })

	attach.SendBytes([]byte{0x1b, '[', 'B'})
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 4 })

	attach.SendBytes([]byte{0x1b, '[', 'A'})
	waitForAttachLessTopLine(t, h, attach, 2*time.Second, func(line int) bool { return line == 3 })
}

func waitForAttachLessTopLine(t *testing.T, h *ptytest.Harness, attach *ptytest.PTYSession, timeout time.Duration, want func(int) bool) int {
	t.Helper()
	deadline := h.Clock().Now().Add(timeout)
	for !h.Clock().Now().After(deadline) {
		if line, ok := attachVisibleLessTopLine(attach.Screen()); ok && want(line) {
			return line
		}
		h.Advance(25 * time.Millisecond)
	}
	screen := attach.Screen()
	if line, ok := attachVisibleLessTopLine(screen); ok && want(line) {
		return line
	}
	t.Fatalf("expected attach less top line to match within %v; got:\n%s", timeout, screen.String())
	return 0
}

func attachVisibleLessTopLine(screen ptytest.Screen) (int, bool) {
	for _, row := range screen.Lines {
		matches := lessVisibleLinePattern.FindStringSubmatch(row)
		if len(matches) != 2 {
			continue
		}
		line, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, false
		}
		return line, true
	}
	return 0, false
}

func lessTestShell(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "less-repro.txt")
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, fmt.Sprintf("LESS-LINE-%03d", i))
	}
	if err := os.WriteFile(dataPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write less data: %v", err)
	}

	scriptPath := filepath.Join(dir, "less-repro.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
export TERM=xterm-256color
exec less %q
`, dataPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write less shell: %v", err)
	}
	return scriptPath
}
