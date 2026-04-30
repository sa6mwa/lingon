//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestHostLocalPTYColonUnderlineResetClearsFollowingOutput(t *testing.T) {
	shell := writePromptShell(t, "PROMPT> ")
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "sgr_colon_underline_reset",
		SessionName: "sgr_colon_underline_reset",
		Shell:       shell,
		Cols:        80,
		Rows:        12,
	})

	waitForHost(t, h, "sgr_colon_underline_reset", 3*time.Second)
	host.Send("printf '\\033[4mUNDER\\033[4:0mPLAIN_MARKER\\n'\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("UNDERPLAIN_MARKER") {
			return fmt.Errorf("waiting for SGR marker")
		}
		return nil
	})

	snap := host.Snapshot()
	row, col := findSnapshotText(snap, "UNDERPLAIN_MARKER")
	if row == 0 {
		t.Fatalf("marker not found in snapshot:\n%s", host.Screen().String())
	}
	for offset := 0; offset < len("UNDER"); offset++ {
		cell, ok := snapshotCellAt(snap, row, col+offset)
		if !ok || cell.Mode&terminal.ModeUnderline == 0 {
			t.Fatalf("expected UNDER cell row=%d col=%d to be underlined", row, col+offset)
		}
	}
	for offset := len("UNDER"); offset < len("UNDERPLAIN_MARKER"); offset++ {
		cell, ok := snapshotCellAt(snap, row, col+offset)
		if !ok {
			t.Fatalf("missing marker cell row=%d col=%d", row, col+offset)
		}
		if cell.Mode&terminal.ModeUnderline != 0 {
			t.Fatalf("expected PLAIN_MARKER cell row=%d col=%d to have underline cleared", row, col+offset)
		}
	}
}

func TestHostLocalPTYAltScreenExitRestoresAttributesForLessLikePrograms(t *testing.T) {
	shell := writePromptShell(t, "PROMPT> ")
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "alt_screen_underline_restore",
		SessionName: "alt_screen_underline_restore",
		Shell:       shell,
		Cols:        80,
		Rows:        12,
	})

	waitForHost(t, h, "alt_screen_underline_restore", 3*time.Second)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for prompt")
		}
		return nil
	})
	host.Send("printf '\\033[?1049h\\033[4mLESS_STATUS\\033[?1049lAFTER_LESS_MARKER\\n'\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("AFTER_LESS_MARKER") {
			return fmt.Errorf("waiting for post-alt-screen marker")
		}
		return nil
	})

	assertSnapshotTextNotUnderlined(t, host.Snapshot(), host.Screen().String(), "AFTER_LESS_MARKER")
}

func TestHostLocalPTYPrivateCSIGreaterMDoesNotRenderUnderlined(t *testing.T) {
	shell := writePromptShell(t, "PROMPT> ")
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "private_csi_greater_m",
		SessionName: "private_csi_greater_m",
		Shell:       shell,
		Cols:        120,
		Rows:        24,
	})

	waitForHost(t, h, "private_csi_greater_m", 3*time.Second)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for prompt")
		}
		return nil
	})
	host.Send("printf '\\033[>4;mPRIVATE_CSI_MARKER\\n'\n")
	eventuallyWithClock(t, h.Clock(), 5*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PRIVATE_CSI_MARKER") {
			return fmt.Errorf("waiting for private CSI marker")
		}
		return nil
	})

	snap := host.Snapshot()
	screen := host.Screen().String()
	assertSnapshotTextPlain(t, snap, screen, "PRIVATE_CSI_MARKER")
}

func TestHostLocalPTYLateOuterOSCResponseDoesNotCorruptNextCommand(t *testing.T) {
	shell := writePromptShell(t, "PROMPT> ")
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "late_outer_osc_response",
		SessionName: "late_outer_osc_response",
		Shell:       shell,
		Cols:        120,
		Rows:        24,
	})

	waitForHost(t, h, "late_outer_osc_response", 3*time.Second)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for prompt")
		}
		return nil
	})

	host.Send("\x1b]10;rgb:ffff/ffff/ffff\x07")
	host.Send("echo AFTER_LATE_OSC\n")
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "AFTER_LATE_OSC") {
			return fmt.Errorf("waiting for command output:\n%s", screen)
		}
		if strings.Contains(screen, "rgb:ffff") || strings.Contains(screen, "not found") {
			return fmt.Errorf("late OSC response leaked into shell input:\n%s", screen)
		}
		return nil
	})
}

func findSnapshotText(snap terminal.Snapshot, text string) (int, int) {
	rows := snapshotRows(snap)
	for row, line := range rows {
		col := findStringColumn(line, text)
		if col >= 0 {
			return row + 1, col + 1
		}
	}
	return 0, 0
}

func writePromptShell(t *testing.T, prompt string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt-shell")
	script := fmt.Sprintf("#!/bin/sh\nexport PS1=%q\nexec /bin/sh -i\n", prompt)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write prompt shell: %v", err)
	}
	return path
}

func assertSnapshotTextNotUnderlined(t *testing.T, snap terminal.Snapshot, screen, text string) {
	t.Helper()
	assertSnapshotTextModeClear(t, snap, screen, text, terminal.ModeUnderline, "underlined")
}

func assertSnapshotTextPlain(t *testing.T, snap terminal.Snapshot, screen, text string) {
	t.Helper()
	assertSnapshotTextModeClear(t, snap, screen, text, terminal.ModeUnderline|terminal.ModeItalic, "styled")
}

func assertSnapshotTextModeClear(t *testing.T, snap terminal.Snapshot, screen, text string, mode int16, description string) {
	t.Helper()
	row, col := findSnapshotText(snap, text)
	if row == 0 {
		t.Fatalf("%q not found in snapshot:\n%s", text, screen)
	}
	for offset := 0; offset < len(text); offset++ {
		cell, ok := snapshotCellAt(snap, row, col+offset)
		if !ok {
			t.Fatalf("missing %q cell row=%d col=%d", text, row, col+offset)
		}
		if cell.Mode&mode != 0 {
			t.Fatalf("expected %q cell row=%d col=%d to not be %s", text, row, col+offset, description)
		}
	}
}

func snapshotText(snap terminal.Snapshot) string {
	return strings.Join(snapshotRows(snap), "\n")
}

func findStringColumn(s, needle string) int {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
