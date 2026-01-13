package emu

import (
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
