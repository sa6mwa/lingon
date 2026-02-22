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

func TestHostScrollbackCorpusKeepsTrailingBlankAttrsDefaultWhileScrolling(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	const (
		cols = 96
		rows = 22
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host_trailing_attrs",
		SessionName: "scroll_host_trailing_attrs",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})

	waitForHost(t, h, "scroll_host_trailing_attrs", 3*time.Second)

	// Corpus intentionally mixes ANSI styles and occasional wide glyph output.
	// It does not emit trailing spaces, so trailing viewport cells should stay
	// default after scrollback renders.
	host.Send("i=1; while [ $i -le 260 ]; do c=$((31 + (i % 6))); printf \"\\033[%smL%03d\\033[0m\" \"$c\" \"$i\"; if [ $((i % 3)) -eq 0 ]; then printf \" \\033[48;5;24m\\342\\234\\205\\033[0m\"; fi; if [ $((i % 4)) -eq 0 ]; then printf \" \\033[7mINV\\033[0m\"; fi; printf \"\\n\"; i=$((i+1)); done\n")
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("L260") {
			return fmt.Errorf("waiting for styled corpus output")
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)

	for i := 0; i < 150; i++ {
		host.SendBytes([]byte{0x1b, '[', 'A'})
		advanceTestClock(h.Clock(), 18*time.Millisecond)
		if i%10 == 0 {
			assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
		}
	}

	for i := 0; i < 16; i++ {
		host.SendBytes([]byte{0x1b, '[', '5', '~'})
		advanceTestClock(h.Clock(), 45*time.Millisecond)
		assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
	}

	for i := 0; i < 70; i++ {
		host.SendBytes([]byte{0x1b, '[', 'B'})
		advanceTestClock(h.Clock(), 18*time.Millisecond)
		if i%10 == 0 {
			assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
		}
	}
}

func TestHostScrollbackPSAuxTrailingBlankAttrsDefaultWhileScrolling(t *testing.T) {
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
		SessionID:   "scroll_host_ps_aux_attrs",
		SessionName: "scroll_host_ps_aux_attrs",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})

	waitForHost(t, h, "scroll_host_ps_aux_attrs", 3*time.Second)

	host.Send("COLUMNS=120 sh -c 'ps aux 2>/dev/null || ps -ef' | sed -n '1,120p'; echo __PS_DONE__\n")
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("__PS_DONE__") {
			return fmt.Errorf("waiting for ps completion marker")
		}
		return nil
	})

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)

	for i := 0; i < 140; i++ {
		host.SendBytes([]byte{0x1b, '[', 'A'})
		advanceTestClock(h.Clock(), 18*time.Millisecond)
		if i%12 == 0 {
			assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
		}
	}

	for i := 0; i < 8; i++ {
		host.SendBytes([]byte{0x1b, '[', '5', '~'})
		advanceTestClock(h.Clock(), 45*time.Millisecond)
		assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
	}

	for i := 0; i < 70; i++ {
		host.SendBytes([]byte{0x1b, '[', 'B'})
		advanceTestClock(h.Clock(), 18*time.Millisecond)
		if i%10 == 0 {
			assertScrollbackTrailingBlankAttrsDefault(t, host, cols, rows)
		}
	}
}

func assertScrollbackTrailingBlankAttrsDefault(t *testing.T, host *ptytest.PTYSession, cols, rows int) {
	t.Helper()
	snap := host.Snapshot()
	lines := snapshotRows(snap)
	// Row 1 is the scrollback status banner and intentionally styled.
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
			t.Fatalf("trailing attrs leak in scrollback row=%d col=%d last=%d rune=%q mode=%d fg=%#x bg=%#x line=%q", row, col, lastContentCol, cell.Rune, cell.Mode, cell.FG, cell.BG, line)
		}
	}
}
