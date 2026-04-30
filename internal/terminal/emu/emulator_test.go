package emu

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestBasicWriteSnapshot(t *testing.T) {
	emu := New(4, 2)
	if err := emu.Write([]byte("ab")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	snap, err := emu.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := cellRune(snap, 0, 0); got != 'a' {
		t.Fatalf("cell(0,0) = %q", got)
	}
	if got := cellRune(snap, 1, 0); got != 'b' {
		t.Fatalf("cell(1,0) = %q", got)
	}
}

func TestWrapAndScroll(t *testing.T) {
	emu := New(3, 2)
	if err := emu.Write([]byte("abcdefg")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	snap, _ := emu.Snapshot()
	if row := rowString(snap, 0); row != "def" {
		t.Fatalf("row0 = %q", row)
	}
	if row := rowString(snap, 1); row != "g  " {
		t.Fatalf("row1 = %q", row)
	}
}

func TestCursorMovement(t *testing.T) {
	emu := New(5, 1)
	_ = emu.Write([]byte("abc"))
	_ = emu.Write([]byte("\x1b[2D"))
	_ = emu.Write([]byte("Z"))
	snap, _ := emu.Snapshot()
	if got := rowString(snap, 0); got[:3] != "aZc" {
		t.Fatalf("row = %q", got)
	}
}

func TestEraseLine(t *testing.T) {
	emu := New(5, 1)
	_ = emu.Write([]byte("hello"))
	_ = emu.Write([]byte("\x1b[2K"))
	snap, _ := emu.Snapshot()
	if row := rowString(snap, 0); row != "     " {
		t.Fatalf("row = %q", row)
	}
}

func TestEraseLineResetsAttributes(t *testing.T) {
	emu := New(4, 1)
	_ = emu.Write([]byte("\x1b[41m"))
	_ = emu.Write([]byte("abcd"))
	_ = emu.Write([]byte("\x1b[2K"))
	snap, _ := emu.Snapshot()
	for x := 0; x < 4; x++ {
		cell := cellAt(snap, x, 0)
		if cell.FG != terminal.ColorDefault || cell.BG == terminal.ColorDefault || cell.Mode != 0 {
			t.Fatalf("cell(%d,0) = fg %v bg %v mode %v", x, cell.FG, cell.BG, cell.Mode)
		}
	}
}

func TestEraseLineDefaultsWhenNoBackground(t *testing.T) {
	emu := New(3, 1)
	_ = emu.Write([]byte("abc"))
	_ = emu.Write([]byte("\x1b[2K"))
	snap, _ := emu.Snapshot()
	for x := 0; x < 3; x++ {
		cell := cellAt(snap, x, 0)
		if cell.FG != terminal.ColorDefault || cell.BG != terminal.ColorDefault || cell.Mode != 0 {
			t.Fatalf("cell(%d,0) = fg %v bg %v mode %v", x, cell.FG, cell.BG, cell.Mode)
		}
	}
}

func TestScrollResetsAttributes(t *testing.T) {
	emu := New(3, 2)
	_ = emu.Write([]byte("\x1b[44m"))
	_ = emu.Write([]byte("123\n456\n"))
	snap, _ := emu.Snapshot()
	for x := 0; x < 3; x++ {
		cell := cellAt(snap, x, 1)
		if cell.FG != terminal.ColorDefault || cell.BG == terminal.ColorDefault || cell.Mode != 0 {
			t.Fatalf("cell(%d,1) = fg %v bg %v mode %v", x, cell.FG, cell.BG, cell.Mode)
		}
	}
}

func TestAltScreenSwitch(t *testing.T) {
	emu := New(5, 1)
	_ = emu.Write([]byte("main"))
	_ = emu.Write([]byte("\x1b[?1049h"))
	_ = emu.Write([]byte("alt"))
	snap, _ := emu.Snapshot()
	if got := rowString(snap, 0); got[:3] != "alt" {
		t.Fatalf("alt row = %q", got)
	}
	_ = emu.Write([]byte("\x1b[?1049l"))
	snap, _ = emu.Snapshot()
	if got := rowString(snap, 0); got[:4] != "main" {
		t.Fatalf("main row = %q", got)
	}
}

func TestSGRColors(t *testing.T) {
	emu := New(2, 1)
	_ = emu.Write([]byte("\x1b[31mA"))
	snap, _ := emu.Snapshot()
	cell := cellAt(snap, 0, 0)
	if cell.Rune != 'A' {
		t.Fatalf("rune = %q", cell.Rune)
	}
	if cell.FG == terminal.ColorDefault {
		t.Fatalf("expected fg color set")
	}
}

func TestSGREmptyResetsAttributes(t *testing.T) {
	emu := New(2, 1)
	_ = emu.Write([]byte("\x1b[7mA\x1b[mB"))
	snap, _ := emu.Snapshot()
	cellA := cellAt(snap, 0, 0)
	cellB := cellAt(snap, 1, 0)
	if cellA.Mode&terminal.ModeInverse == 0 {
		t.Fatalf("expected inverse on first cell")
	}
	if cellB.Mode&terminal.ModeInverse != 0 {
		t.Fatalf("expected inverse cleared on second cell")
	}
}

func TestSGRColonUnderlineStyleResetClearsUnderline(t *testing.T) {
	emu := New(2, 1)
	_ = emu.Write([]byte("\x1b[4mA\x1b[4:0mB"))
	snap, _ := emu.Snapshot()
	cellA := cellAt(snap, 0, 0)
	cellB := cellAt(snap, 1, 0)
	if cellA.Mode&terminal.ModeUnderline == 0 {
		t.Fatalf("expected underline on first cell")
	}
	if cellB.Mode&terminal.ModeUnderline != 0 {
		t.Fatalf("expected colon-form underline reset to clear second cell underline")
	}
}

func TestSGRColonUnderlineStyleEnablesUnderline(t *testing.T) {
	emu := New(1, 1)
	_ = emu.Write([]byte("\x1b[4:3mA"))
	snap, _ := emu.Snapshot()
	cell := cellAt(snap, 0, 0)
	if cell.Mode&terminal.ModeUnderline == 0 {
		t.Fatalf("expected colon-form underline style to enable underline")
	}
}

func TestPrivateCSIGreaterMDoesNotEnableUnderline(t *testing.T) {
	emu := New(1, 1)
	_ = emu.Write([]byte("\x1b[>4;mA"))
	snap, _ := emu.Snapshot()
	cell := cellAt(snap, 0, 0)
	if cell.Mode&terminal.ModeUnderline != 0 {
		t.Fatalf("expected private CSI > m sequence to not enable underline")
	}
}

func TestAltScreen1049RestoresSavedAttributes(t *testing.T) {
	emu := New(5, 1)
	_ = emu.Write([]byte("\x1b[?1049h\x1b[4mALT\x1b[?1049lPLAIN"))
	snap, _ := emu.Snapshot()
	for x := 0; x < 5; x++ {
		cell := cellAt(snap, x, 0)
		if cell.Mode&terminal.ModeUnderline != 0 {
			t.Fatalf("expected main-screen cell %d to not inherit alt-screen underline", x)
		}
	}
}

func TestTabStops(t *testing.T) {
	emu := New(10, 1)
	_ = emu.Write([]byte("a\tb"))
	snap, _ := emu.Snapshot()
	if got := cellRune(snap, 0, 0); got != 'a' {
		t.Fatalf("cell0 = %q", got)
	}
	if got := cellRune(snap, 8, 0); got != 'b' {
		t.Fatalf("cell8 = %q", got)
	}
}

func TestLineDrawingCharset(t *testing.T) {
	emu := New(2, 1)
	_ = emu.Write([]byte("\x1b)0\x0eq\x0f"))
	snap, _ := emu.Snapshot()
	if got := cellRune(snap, 0, 0); got != '─' {
		t.Fatalf("cell0 = %q", got)
	}
}

func TestCRLFMovesToNextLine(t *testing.T) {
	emu := New(4, 3)
	_ = emu.Write([]byte("one\r\ntwo\r\n"))
	snap, _ := emu.Snapshot()
	if row := rowString(snap, 0); row[:3] != "one" {
		t.Fatalf("row0 = %q", row)
	}
	if row := rowString(snap, 1); row[:3] != "two" {
		t.Fatalf("row1 = %q", row)
	}
}

func TestResizePreservesScrollback(t *testing.T) {
	emu := New(8, 3)
	emu.SetScrollbackLimit(200)
	_ = emu.Write([]byte("LINE-001\nLINE-002\nLINE-003\nLINE-004\nLINE-005\nLINE-006\n"))
	before := emu.ScrollbackSnapshot()
	if len(before) == 0 {
		t.Fatalf("expected scrollback before resize")
	}
	if !scrollbackContains(before, "LINE-001") {
		t.Fatalf("expected early history present before resize")
	}

	emu.Resize(6, 4)

	after := emu.ScrollbackSnapshot()
	if len(after) == 0 {
		t.Fatalf("expected scrollback retained after resize")
	}
	if !scrollbackContains(after, "LINE-001") {
		t.Fatalf("expected early history retained after resize")
	}
}

func TestScrollbackCapturesTopAnchoredScrollRegion(t *testing.T) {
	emu := New(16, 4)
	emu.SetScrollbackLimit(50)
	_ = emu.Write([]byte("\x1b[1;3r"))
	_ = emu.Write([]byte("LINE-001\r\nLINE-002\r\nLINE-003\r\nLINE-004\r\n"))

	scrollback := emu.ScrollbackSnapshot()
	if len(scrollback) == 0 {
		t.Fatalf("expected scrollback rows for top-anchored scroll region")
	}
	if !scrollbackContains(scrollback, "LINE-001") {
		t.Fatalf("expected oldest scrolled row captured in scrollback")
	}
}

func TestScrollbackIgnoresMidScreenScrollRegion(t *testing.T) {
	emu := New(16, 5)
	emu.SetScrollbackLimit(50)
	_ = emu.Write([]byte("\x1b[2;4r"))
	_ = emu.Write([]byte("\x1b[2;1H"))
	_ = emu.Write([]byte("LINE-001\r\nLINE-002\r\nLINE-003\r\nLINE-004\r\n"))

	scrollback := emu.ScrollbackSnapshot()
	if len(scrollback) != 0 {
		t.Fatalf("expected no scrollback rows for mid-screen scroll region, got %d", len(scrollback))
	}
}

func scrollbackContains(rows []terminal.ScrollbackRow, token string) bool {
	for _, row := range rows {
		var b strings.Builder
		for _, c := range row.Cells {
			b.WriteRune(c.Rune)
		}
		if strings.Contains(b.String(), token) {
			return true
		}
	}
	return false
}

func cellRune(s terminal.Snapshot, x, y int) rune {
	cell := cellAt(s, x, y)
	return cell.Rune
}

func cellAt(s terminal.Snapshot, x, y int) terminal.Cell {
	cell, err := s.CellAt(x, y)
	if err != nil {
		return terminal.Cell{Rune: ' '}
	}
	return cell
}

func rowString(s terminal.Snapshot, y int) string {
	row := make([]rune, 0, 32)
	for x := 0; ; x++ {
		cell, err := s.CellAt(x, y)
		if err != nil {
			break
		}
		row = append(row, cell.Rune)
	}
	return string(row)
}
