package session_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestHostPlainModePSAuxTrailingBlankAttrsDefault(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	const (
		cols = 120
		rows = 28
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "plain_host_ps_aux_attrs",
		SessionName: "plain_host_ps_aux_attrs",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})

	waitForHost(t, h, "plain_host_ps_aux_attrs", 3*time.Second)

	// Drive several normal-mode refreshes with ps-style tabular output where
	// row widths vary significantly.
	host.Send("COLUMNS=120 sh -c 'ps aux 2>/dev/null || ps -ef' | sed -n '1,24p'; echo __PS_DONE__\n")
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("__PS_DONE__") {
			return fmt.Errorf("waiting for ps completion marker")
		}
		return nil
	})

	host.Send("COLUMNS=120 sh -c 'ps aux 2>/dev/null || ps -ef' | sed -n '1,24p'; echo __PS_DONE__\n")
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("__PS_DONE__") {
			return fmt.Errorf("waiting for second ps completion marker")
		}
		return nil
	})

	assertPlainTrailingBlankAttrsDefault(t, host, cols, rows)
}

func assertPlainTrailingBlankAttrsDefault(t *testing.T, host *ptytest.PTYSession, cols, rows int) {
	t.Helper()
	foundRowWithTail := false
	snap := host.Snapshot()
	lines := snapshotRows(snap)

	// Row 1 may be tab/status overlay depending on runtime state.
	for row := 2; row <= rows; row++ {
		lastContentCol := 0
		for col := 1; col <= cols; col++ {
			cell, ok := snapshotCellAt(snap, row, col)
			if !ok {
				continue
			}
			if cell.Mode&terminal.ModeHidden != 0 {
				continue
			}
			width := 1
			if cell.Grapheme != "" {
				if w := runewidth.StringWidth(cell.Grapheme); w > 1 {
					width = w
				}
				end := col + width - 1
				if end > cols {
					end = cols
				}
				if end > lastContentCol {
					lastContentCol = end
				}
				continue
			}
			if cell.Rune != 0 && cell.Rune != ' ' {
				if w := runewidth.RuneWidth(cell.Rune); w > 1 {
					width = w
				}
				end := col + width - 1
				if end > cols {
					end = cols
				}
				if end > lastContentCol {
					lastContentCol = end
				}
			}
		}

		if lastContentCol > 0 && lastContentCol < cols {
			foundRowWithTail = true
		}
		for col := lastContentCol + 1; col <= cols; col++ {
			cell, ok := snapshotCellAt(snap, row, col)
			if !ok {
				continue
			}
			if cell.Mode == 0 && cell.FG == terminal.ColorDefault && cell.BG == terminal.ColorDefault {
				continue
			}
			line := ""
			if row-1 >= 0 && row-1 < len(lines) {
				line = lines[row-1]
			}
			t.Fatalf("plain-mode trailing attrs leak row=%d col=%d last=%d rune=%q mode=%d fg=%#x bg=%#x line=%q", row, col, lastContentCol, cell.Rune, cell.Mode, cell.FG, cell.BG, line)
		}
	}

	if !foundRowWithTail {
		t.Fatalf("expected at least one partially filled row to validate trailing blank attrs")
	}
}
