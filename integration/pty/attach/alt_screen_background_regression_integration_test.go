//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestAttachAltScreenExitDoesNotLeakPromptBackgroundToRestoredPromptLine(t *testing.T) {
	const (
		cols = 80
		rows = 12

		sessionID = "attach-alt-screen-background"
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   sessionID,
		SessionName: sessionID,
		Shell:       "/bin/sh",
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:      sessionID,
		RequestControl: true,
		Cols:           cols,
		Rows:           rows,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)

	script := filepath.Join(t.TempDir(), "alt-screen-background")
	const body = "#!/bin/sh\nprintf '\\033[12;1H\\033[44m\\033[?1049h\\033[12;80HX\\033[?1049l\\033[44mC89\\033[0m PROMPT_MARKER'\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write alternate-screen fixture: %v", err)
	}
	host.Send("exec " + script + "\n")

	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("PROMPT_MARKER") {
			return fmt.Errorf("waiting for restored primary screen:\n%s", screen.String())
		}
		if cursor := attach.Cursor(); cursor.Row != rows || cursor.Col != len("C89 PROMPT_MARKER")+1 {
			return fmt.Errorf("expected prompt cursor at row=%d col=%d, got row=%d col=%d\nscreen:\n%s", rows, len("C89 PROMPT_MARKER")+1, cursor.Row, cursor.Col, screen.String())
		}
		return nil
	})

	promptCell, ok := attach.CellAt(rows, 1)
	if !ok || promptCell.BG == terminal.ColorDefault {
		t.Fatalf("expected blue prompt cell at row %d col 1, got %+v (ok=%t)", rows, promptCell, ok)
	}
	restoredCell, ok := attach.CellAt(rows, cols)
	if !ok {
		t.Fatalf("missing restored prompt-line cell at row %d col %d", rows, cols)
	}
	if restoredCell.BG != terminal.ColorDefault {
		t.Fatalf("restored prompt line inherited prompt background: %#x", restoredCell.BG)
	}
}
