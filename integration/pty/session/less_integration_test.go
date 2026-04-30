//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostLessArrowKeysFollowApplicationCursorMode(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less not installed")
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "less-host",
		SessionName: "less-host",
		Shell:       lessTestShell(t),
		Cols:        100,
		Rows:        20,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "less-host", 5*time.Second)
	host.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(1), "LESS-LINE-002") {
			return fmt.Errorf("waiting for less screen:\n%s", screen.String())
		}
		return nil
	})

	_ = host.DrainRaw()

	host.SendBytes([]byte("j"))
	host.Eventually(2*time.Second, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(1), "LESS-LINE-003") {
			return fmt.Errorf("expected j to scroll less down one line:\n%s", screen.String())
		}
		return nil
	})

	host.SendBytes([]byte{0x1b, '[', 'B'})
	host.Eventually(2*time.Second, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(1), "LESS-LINE-004") {
			return fmt.Errorf("expected down arrow to scroll less down one line:\n%s", screen.String())
		}
		return nil
	})

	host.SendBytes([]byte{0x1b, '[', 'A'})
	host.Eventually(2*time.Second, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.Row(1), "LESS-LINE-003") {
			return fmt.Errorf("expected up arrow to scroll less up one line:\n%s", screen.String())
		}
		return nil
	})

	host.SendBytes([]byte("q"))
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
