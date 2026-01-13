package render

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestSgrKeepsNonBoldIndexed(t *testing.T) {
	attr := renderAttr{
		mode: 0,
		fg:   terminal.ColorIndexed | 7,
		bg:   terminal.ColorDefault,
	}
	got := sgr(attr)
	if !strings.Contains(got, "37") {
		t.Fatalf("expected indexed color 7, got %q", got)
	}
}

func TestSnapshotViewportDeltaSkipsClear(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 2,
		Runes: []uint32{
			'a', 'b',
			'c', 'd',
		},
		Modes: []int32{0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 0,
			Y: 0,
		},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 2,
		Runes: []uint32{
			'a', 'b',
			'c', 'x',
		},
		Modes: []int32{0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 1,
			Y: 1,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportDelta(&buf, prev, next, 2, 2); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}
	if strings.Contains(buf.String(), ansiClearScreen) {
		t.Fatalf("unexpected clear screen in delta render")
	}
}

func TestSnapshotViewportDeltaOriginShiftSkipsClear(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 4,
		Rows: 4,
		Runes: []uint32{
			'a', 'b', 'c', 'd',
			'e', 'f', 'g', 'h',
			'i', 'j', 'k', 'l',
			'm', 'n', 'o', 'p',
		},
		Modes: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 0,
			Y: 0,
		},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 4,
		Rows: 4,
		Runes: []uint32{
			'a', 'b', 'c', 'd',
			'e', 'f', 'g', 'h',
			'i', 'j', 'k', 'l',
			'm', 'n', 'o', 'p',
		},
		Modes: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 3,
			Y: 3,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportDelta(&buf, prev, next, 2, 2); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}
	if strings.Contains(buf.String(), ansiClearScreen) {
		t.Fatalf("unexpected clear screen on origin shift")
	}
}

func TestSgrInverseUsesInverseCodeWithoutSwapping(t *testing.T) {
	attr := renderAttr{
		mode: int32(terminal.ModeInverse),
		fg:   terminal.ColorIndexed | 2,
		bg:   terminal.ColorIndexed | 4,
	}
	got := sgr(attr)
	seq := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b["), "m")
	found := false
	for _, part := range strings.Split(seq, ";") {
		if part == "7" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inverse SGR code in %q", got)
	}
	if !strings.Contains(got, "32") || !strings.Contains(got, "44") {
		t.Fatalf("expected original colors preserved, got %q", got)
	}
}

func TestSgrInverseWithDefaultUsesInverseCode(t *testing.T) {
	attr := renderAttr{
		mode: int32(terminal.ModeInverse),
		fg:   terminal.ColorDefault,
		bg:   terminal.ColorIndexed | 2,
	}
	got := sgr(attr)
	seq := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b["), "m")
	found := false
	for _, part := range strings.Split(seq, ";") {
		if part == "7" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inverse SGR code in %q", got)
	}
	if !strings.Contains(got, "42") {
		t.Fatalf("expected background color preserved, got %q", got)
	}
	if !strings.Contains(got, "39") {
		t.Fatalf("expected default fg code, got %q", got)
	}
}

func TestColorCodeIndexedUsesAnsiPalette(t *testing.T) {
	if got := strings.Join(colorCode(true, terminal.ColorIndexed|2), ";"); got != "32" {
		t.Fatalf("expected ansi fg 32 for index 2, got %q", got)
	}
	if got := strings.Join(colorCode(false, terminal.ColorIndexed|2), ";"); got != "42" {
		t.Fatalf("expected ansi bg 42 for index 2, got %q", got)
	}
	if got := strings.Join(colorCode(true, terminal.ColorIndexed|12), ";"); got != "94" {
		t.Fatalf("expected ansi fg 94 for index 12, got %q", got)
	}
	if got := strings.Join(colorCode(false, terminal.ColorIndexed|12), ";"); got != "104" {
		t.Fatalf("expected ansi bg 104 for index 12, got %q", got)
	}
}

func TestColorCodeIndexedUses256ForExtended(t *testing.T) {
	if got := strings.Join(colorCode(true, terminal.ColorIndexed256|16), ";"); got != "38;5;16" {
		t.Fatalf("expected 256 fg for index 16, got %q", got)
	}
	if got := strings.Join(colorCode(false, terminal.ColorIndexed256|200), ";"); got != "48;5;200" {
		t.Fatalf("expected 256 bg for index 200, got %q", got)
	}
}

func TestSgrBoldDoesNotPromoteIndexed(t *testing.T) {
	attr := renderAttr{
		mode: int32(terminal.ModeBold),
		fg:   terminal.ColorIndexed | 7,
		bg:   terminal.ColorDefault,
	}
	got := sgr(attr)
	if strings.Contains(got, "97") {
		t.Fatalf("expected bold to keep base indexed color, got %q", got)
	}
	if !strings.Contains(got, "37") {
		t.Fatalf("expected base white (37), got %q", got)
	}
	if !strings.Contains(got, "1") {
		t.Fatalf("expected bold flag preserved, got %q", got)
	}
}

func TestSnapshotViewportResetsRowAttributes(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols: 3,
		Rows: 2,
		Runes: []uint32{
			'A', 'B', 'C',
			'D', 'E', 'F',
		},
		Modes: []int32{
			0, 0, 0,
			0, 0, 0,
		},
		Fg: []uint32{
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
		},
		Bg: []uint32{
			terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2,
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
		},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := Snapshot(&buf, snap); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	e := emu.New(3, 2)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	round, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emu snapshot: %v", err)
	}
	cell, err := round.CellAt(0, 1)
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	if cell.BG != terminal.ColorDefault {
		t.Fatalf("expected row1 bg default, got %d", cell.BG)
	}
}
