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

func TestSnapshotViewportDeltaWritesChangedCellSpanOnly(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 5,
		Rows: 1,
		Runes: []uint32{
			'a', 'b', 'c', 'd', 'e',
		},
		Modes: []int32{0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 2,
			Y: 0,
		},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 5,
		Rows: 1,
		Runes: []uint32{
			'a', 'b', 'X', 'd', 'e',
		},
		Modes: []int32{0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 2,
			Y: 0,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportDelta(&buf, prev, next, 5, 1); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b[1;1H") {
		t.Fatalf("expected no row-start repaint for single-cell change, got %q", out)
	}
	if !strings.Contains(out, "\x1b[1;3H") {
		t.Fatalf("expected update cursor move to changed column, got %q", out)
	}
	if strings.Contains(out, "abXde") {
		t.Fatalf("expected no full-row repaint payload, got %q", out)
	}
}

func TestSnapshotViewportDeltaResetsAttributesBeforeSpanWrite(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 1,
		Runes: []uint32{
			'a', 'b',
		},
		Modes: []int32{0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 1,
			Y: 0,
		},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 1,
		Runes: []uint32{
			'a', 'x',
		},
		Modes: []int32{0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 1,
			Y: 0,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	buf.WriteString("\x1b[41m")
	if err := SnapshotViewportDelta(&buf, prev, next, 2, 1); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}

	e := emu.New(2, 1)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	round, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emu snapshot: %v", err)
	}
	cell, err := round.CellAt(1, 0)
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	if cell.BG != terminal.ColorDefault {
		t.Fatalf("expected default bg after delta reset, got %d", cell.BG)
	}
}

func TestSnapshotViewportDeltaClearsChangedCellToSpace(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 5,
		Rows: 1,
		Runes: []uint32{
			'a', 'b', 'X', 'd', 'e',
		},
		Modes: []int32{0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 2,
			Y: 0,
		},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 5,
		Rows: 1,
		Runes: []uint32{
			'a', 'b', 0, 'd', 'e',
		},
		Modes: []int32{0, 0, 0, 0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 2,
			Y: 0,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportDelta(&buf, prev, next, 5, 1); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b[1;1H") {
		t.Fatalf("expected no full-row repaint while clearing single cell, got %q", out)
	}
	if !strings.Contains(out, "\x1b[1;3H ") {
		t.Fatalf("expected single-space cell clear at changed column, got %q", out)
	}
}

func TestSnapshotViewportDeltaUsesClearLineForTrailingDefaultTail(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols: 10,
		Rows: 1,
		Runes: []uint32{
			'L', 'O', 'N', 'G', '-', 'N', 'A', 'M', 'E', 'X',
		},
		Modes: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Fg: []uint32{
			terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2,
			terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2, terminal.ColorIndexed | 2,
		},
		Bg: []uint32{
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
		},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 10,
		Rows: 1,
		Runes: []uint32{
			'S', 'H', 'O', 'R', 'T', 0, 0, 0, 0, 0,
		},
		Modes: []int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Fg: []uint32{
			terminal.ColorIndexed | 6, terminal.ColorIndexed | 6, terminal.ColorIndexed | 6, terminal.ColorIndexed | 6, terminal.ColorIndexed | 6,
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
		},
		Bg: []uint32{
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
			terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault,
		},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportDelta(&buf, prev, next, 10, 1); err != nil {
		t.Fatalf("SnapshotViewportDelta: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, ansiClearLine) {
		t.Fatalf("expected delta tail clear via EL, got %q", out)
	}
	if strings.Contains(out, "SHORT     ") {
		t.Fatalf("expected no literal trailing-space write when EL is available, got %q", out)
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

func TestSnapshotViewportDeltaMaskTopRowResetsAttributesBeforeSpanWrite(t *testing.T) {
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
			X: 1,
			Y: 1,
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
	buf.WriteString("\x1b[41m")
	if err := SnapshotViewportDeltaMaskTopRow(&buf, prev, next, 2, 2); err != nil {
		t.Fatalf("SnapshotViewportDeltaMaskTopRow: %v", err)
	}

	e := emu.New(2, 2)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	round, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emu snapshot: %v", err)
	}
	cell, err := round.CellAt(1, 1)
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	if cell.BG != terminal.ColorDefault {
		t.Fatalf("expected default bg after masked delta reset, got %d", cell.BG)
	}
}

func TestSnapshotViewportNoClearSkipsClearLineAfterLastColumn(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 1,
		Runes: []uint32{
			'a', 'b',
		},
		Modes: []int32{0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 1,
			Y: 0,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportNoClear(&buf, snap, 2, 1); err != nil {
		t.Fatalf("SnapshotViewportNoClear: %v", err)
	}
	if strings.Contains(buf.String(), ansiClearLine) {
		t.Fatalf("unexpected clear line after writing last column")
	}
}

func TestSnapshotViewportNoClearClearsTrailingDefaultCells(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols: 2,
		Rows: 1,
		Runes: []uint32{
			'a', 0,
		},
		Modes: []int32{0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Cursor: &protocolpb.Cursor{
			X: 0,
			Y: 0,
		},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := SnapshotViewportNoClear(&buf, snap, 2, 1); err != nil {
		t.Fatalf("SnapshotViewportNoClear: %v", err)
	}
	if !strings.Contains(buf.String(), ansiClearLine) {
		t.Fatalf("expected clear line for trailing default cells")
	}
}

func TestBuildRowResetsWhenEndingWithNonDefaultAttr(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:  2,
		Rows:  1,
		Runes: []uint32{'a', 'b'},
		Modes: []int32{0, 0},
		Fg:    []uint32{terminal.ColorDefault, terminal.ColorDefault},
		Bg:    []uint32{terminal.ColorDefault, terminal.ColorIndexed | 1},
		Cursor: &protocolpb.Cursor{
			X: 1,
			Y: 0,
		},
		CursorVisible: true,
	}
	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	row := buildRow(snap, 0, 0, 2, 2, 1, defaultAttr)
	if strings.Contains(row, ansiClearLine) {
		t.Fatalf("unexpected clear line when last column non-default")
	}
	if !strings.HasSuffix(row, ansiReset) {
		t.Fatalf("expected row to end with reset, got %q", row)
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

func TestWideGraphemeSkipsContinuationCell(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:      3,
		Rows:      1,
		Runes:     []uint32{uint32('❌'), 0, uint32('Z')},
		Modes:     []int32{0, 0, 0},
		Fg:        []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Bg:        []uint32{terminal.ColorDefault, terminal.ColorDefault, terminal.ColorDefault},
		Graphemes: []string{"❌️", "", ""},
		Cursor:    &protocolpb.Cursor{X: 2, Y: 0},
	}

	var buf bytes.Buffer
	if err := SnapshotViewportNoClear(&buf, snap, 3, 1); err != nil {
		t.Fatalf("SnapshotViewportNoClear: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "❌️ Z") {
		t.Fatalf("unexpected continuation space in output: %q", out)
	}
	if !strings.Contains(out, "❌️Z") {
		t.Fatalf("expected grapheme to join next cell: %q", out)
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

func TestSnapshotViewportSkipTopRowReservesTopOverlayRow(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols: 3,
		Rows: 3,
		Runes: []uint32{
			'A', 'A', 'A',
			'B', 'B', 'B',
			'C', 'C', 'C',
		},
		Modes:         make([]int32, 9),
		Fg:            make([]uint32, 9),
		Bg:            make([]uint32, 9),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	for i := range snap.Fg {
		snap.Fg[i] = terminal.ColorDefault
		snap.Bg[i] = terminal.ColorDefault
	}

	var buf bytes.Buffer
	if err := SnapshotViewportSkipTopRow(&buf, snap, 3, 3); err != nil {
		t.Fatalf("SnapshotViewportSkipTopRow: %v", err)
	}
	e := emu.New(3, 3)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	round, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emu snapshot: %v", err)
	}
	if got := rowRunes(t, round, 0); got != "   " {
		t.Fatalf("expected reserved overlay row at top, got %q", got)
	}
	if got := rowRunes(t, round, 1); got != "AAA" {
		t.Fatalf("expected source row 1 shifted to row 2, got %q", got)
	}
	if got := rowRunes(t, round, 2); got != "BBB" {
		t.Fatalf("expected source row 2 shifted to row 3, got %q", got)
	}
}

func TestSnapshotViewportNoClearSkipTopRowReservesTopOverlayRow(t *testing.T) {
	base := &protocolpb.Snapshot{
		Cols: 3,
		Rows: 3,
		Runes: []uint32{
			'X', 'X', 'X',
			'Y', 'Y', 'Y',
			'Z', 'Z', 'Z',
		},
		Modes:         make([]int32, 9),
		Fg:            make([]uint32, 9),
		Bg:            make([]uint32, 9),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	next := &protocolpb.Snapshot{
		Cols: 3,
		Rows: 3,
		Runes: []uint32{
			'A', 'A', 'A',
			'B', 'B', 'B',
			'C', 'C', 'C',
		},
		Modes:         make([]int32, 9),
		Fg:            make([]uint32, 9),
		Bg:            make([]uint32, 9),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	for i := range base.Fg {
		base.Fg[i] = terminal.ColorDefault
		base.Bg[i] = terminal.ColorDefault
		next.Fg[i] = terminal.ColorDefault
		next.Bg[i] = terminal.ColorDefault
	}

	e := emu.New(3, 3)
	var full bytes.Buffer
	if err := SnapshotViewport(&full, base, 3, 3); err != nil {
		t.Fatalf("SnapshotViewport: %v", err)
	}
	if err := e.Write(full.Bytes()); err != nil {
		t.Fatalf("emu write full: %v", err)
	}

	var delta bytes.Buffer
	if err := SnapshotViewportNoClearSkipTopRow(&delta, next, 3, 3); err != nil {
		t.Fatalf("SnapshotViewportNoClearSkipTopRow: %v", err)
	}
	if err := e.Write(delta.Bytes()); err != nil {
		t.Fatalf("emu write delta: %v", err)
	}
	round, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emu snapshot: %v", err)
	}
	if got := rowRunes(t, round, 0); got != "XXX" {
		t.Fatalf("expected top row preserved for overlay composition, got %q", got)
	}
	if got := rowRunes(t, round, 1); got != "AAA" {
		t.Fatalf("expected source row 1 shifted to row 2, got %q", got)
	}
	if got := rowRunes(t, round, 2); got != "BBB" {
		t.Fatalf("expected source row 2 shifted to row 3, got %q", got)
	}
}

func rowRunes(t *testing.T, snap terminal.Snapshot, y int) string {
	t.Helper()
	var b strings.Builder
	for x := 0; x < int(snap.Cols); x++ {
		cell, err := snap.CellAt(x, y)
		if err != nil {
			t.Fatalf("cell(%d,%d): %v", x, y, err)
		}
		r := rune(cell.Rune)
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}
