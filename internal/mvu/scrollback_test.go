package mvu

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func makeLiveSnapshot(cols, rows int) *protocolpb.Snapshot {
	size := cols * rows
	return &protocolpb.Snapshot{
		Cols:          uint32(cols),
		Rows:          uint32(rows),
		Runes:         make([]uint32, size),
		Modes:         make([]int32, size),
		Fg:            make([]uint32, size),
		Bg:            make([]uint32, size),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
}

func writeSnapshotRow(snap *protocolpb.Snapshot, row int, text string) {
	if snap == nil || row < 0 || row >= int(snap.Rows) {
		return
	}
	cols := int(snap.Cols)
	for x := 0; x < cols; x++ {
		idx := row*cols + x
		if x < len(text) {
			snap.Runes[idx] = uint32(text[x])
		} else {
			snap.Runes[idx] = uint32(' ')
		}
	}
}

func TestScrollbackPercentBounds(t *testing.T) {
	cases := []struct {
		name     string
		total    int
		viewRows int
		offset   int
		want     int
	}{
		{name: "no scrollback", total: 10, viewRows: 10, offset: 0, want: 100},
		{name: "top", total: 100, viewRows: 20, offset: 80, want: 0},
		{name: "bottom", total: 100, viewRows: 20, offset: 0, want: 100},
		{name: "middle", total: 100, viewRows: 20, offset: 40, want: 50},
		{name: "negative offset clamps", total: 100, viewRows: 20, offset: -10, want: 100},
		{name: "overflow offset clamps", total: 100, viewRows: 20, offset: 500, want: 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ScrollbackPercent(tc.total, tc.viewRows, tc.offset)
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestBuildScrollbackViewFromProtoLiveOnly(t *testing.T) {
	live := makeLiveSnapshot(8, 3)
	writeSnapshotRow(live, 0, "row0")
	writeSnapshotRow(live, 1, "row1")
	writeSnapshotRow(live, 2, "row2")
	live.Cursor = &protocolpb.Cursor{X: 2, Y: 2}
	live.CursorVisible = true

	out := BuildScrollbackViewFromProto(8, 3, nil, live, 0, 0)
	if out == nil {
		t.Fatalf("expected output snapshot")
	}
	if out.Cols != 8 || out.Rows != 3 {
		t.Fatalf("unexpected output size: %dx%d", out.Cols, out.Rows)
	}
	row := rowStringFromSnapshot(out, 2)
	if row[:4] != "row2" {
		t.Fatalf("expected live row preserved, got %q", row)
	}
	if out.Cursor == nil || out.Cursor.X != 0 || out.Cursor.Y != 0 || !out.CursorVisible {
		t.Fatalf("expected anchored visible cursor at 0,0 in scrollback mode, got %+v visible=%v", out.Cursor, out.CursorVisible)
	}
}

func TestBuildScrollbackViewFromProtoWithOffsetKeepsAnchoredCursorVisible(t *testing.T) {
	live := makeLiveSnapshot(8, 2)
	writeSnapshotRow(live, 0, "live0")
	writeSnapshotRow(live, 1, "live1")
	live.Cursor = &protocolpb.Cursor{X: 1, Y: 1}
	live.CursorVisible = true

	scrollback := []*protocolpb.ScrollbackRow{
		{Runes: []uint32{'s', '0'}},
		{Runes: []uint32{'s', '1'}},
		{Runes: []uint32{'s', '2'}},
	}
	out := BuildScrollbackViewFromProto(8, 2, scrollback, live, 2, 0)
	if out == nil {
		t.Fatalf("expected output snapshot")
	}
	if out.Cursor == nil || out.Cursor.X != 0 || out.Cursor.Y != 0 || !out.CursorVisible {
		t.Fatalf("expected anchored visible cursor at 0,0 in offset view, got %+v visible=%v", out.Cursor, out.CursorVisible)
	}
	first := rowStringFromSnapshot(out, 0)
	second := rowStringFromSnapshot(out, 1)
	if first[0:2] != "s1" || second[0:2] != "s2" {
		t.Fatalf("unexpected scrollback rows: %q %q", first, second)
	}
}

func TestBuildScrollbackViewFromProtoOffsetKeepsCursorAnchorStable(t *testing.T) {
	live := makeLiveSnapshot(8, 2)
	writeSnapshotRow(live, 0, "live0")
	writeSnapshotRow(live, 1, "live1")
	live.Cursor = &protocolpb.Cursor{X: 1, Y: 0}
	live.CursorVisible = true

	scrollback := []*protocolpb.ScrollbackRow{
		{Runes: []uint32{'s', '0'}},
		{Runes: []uint32{'s', '1'}},
		{Runes: []uint32{'s', '2'}},
	}
	base := BuildScrollbackViewFromProto(8, 4, scrollback, live, 0, 0)
	older := BuildScrollbackViewFromProto(8, 4, scrollback, live, 1, 0)
	if base.Cursor == nil || older.Cursor == nil {
		t.Fatalf("expected cursor visible in both views")
	}
	if !base.CursorVisible || !older.CursorVisible {
		t.Fatalf("expected cursor visibility preserved")
	}
	if base.Cursor.X != 0 || base.Cursor.Y != 0 {
		t.Fatalf("expected anchored cursor at 0,0 for base view, got x=%d y=%d", base.Cursor.X, base.Cursor.Y)
	}
	if older.Cursor.X != 0 || older.Cursor.Y != 0 {
		t.Fatalf("expected anchored cursor at 0,0 for older view, got x=%d y=%d", older.Cursor.X, older.Cursor.Y)
	}
}

func TestBuildScrollbackViewFromProtoDefaultsToLiveSize(t *testing.T) {
	live := makeLiveSnapshot(6, 4)
	writeSnapshotRow(live, 3, "last")
	out := BuildScrollbackViewFromProto(0, 0, nil, live, 0, 0)
	if out == nil {
		t.Fatalf("expected output snapshot")
	}
	if out.Cols != 6 || out.Rows != 4 {
		t.Fatalf("expected fallback to live size, got %dx%d", out.Cols, out.Rows)
	}
}

func TestBuildScrollbackViewFromTerminalRows(t *testing.T) {
	live := makeLiveSnapshot(6, 2)
	writeSnapshotRow(live, 0, "live0")
	writeSnapshotRow(live, 1, "live1")
	live.Cursor = &protocolpb.Cursor{X: 3, Y: 1}
	live.CursorVisible = true

	rows := []terminal.ScrollbackRow{
		{
			Cells: []terminal.Cell{
				{Rune: 'a', Mode: terminal.ModeBold, FG: 1, BG: 2},
				{Rune: '0'},
			},
		},
		{
			Cells: []terminal.Cell{
				{Rune: 'a'},
				{Rune: '1'},
			},
		},
	}

	out := BuildScrollbackViewFromTerminal(6, 3, rows, live, 0, 0)
	if out == nil {
		t.Fatalf("expected output snapshot")
	}
	if out.Cols != 6 || out.Rows != 3 {
		t.Fatalf("unexpected output size: %dx%d", out.Cols, out.Rows)
	}
	if row := rowStringFromSnapshot(out, 0); row[0:2] != "a1" {
		t.Fatalf("expected first scrollback row, got %q", row)
	}
	if row := rowStringFromSnapshot(out, 1); row[0:5] != "live0" {
		t.Fatalf("expected first live row after scrollback, got %q", row)
	}
	if row := rowStringFromSnapshot(out, 2); row[0:5] != "live1" {
		t.Fatalf("expected second live row after scrollback, got %q", row)
	}
}

func TestBuildScrollbackViewFromTerminalCursorMapping(t *testing.T) {
	live := makeLiveSnapshot(8, 2)
	writeSnapshotRow(live, 0, "live0")
	writeSnapshotRow(live, 1, "live1")
	live.Cursor = &protocolpb.Cursor{X: 2, Y: 0}
	live.CursorVisible = true

	scrollback := []terminal.ScrollbackRow{
		{Cells: []terminal.Cell{{Rune: 's'}, {Rune: '0'}}},
		{Cells: []terminal.Cell{{Rune: 's'}, {Rune: '1'}}},
	}
	out := BuildScrollbackViewFromTerminal(8, 3, scrollback, live, 0, 0)
	if out.Cursor == nil {
		t.Fatalf("expected cursor in output when offset == 0")
	}
	if out.Cursor.X != 0 || out.Cursor.Y != 0 || !out.CursorVisible {
		t.Fatalf("expected anchored visible cursor at 0,0 when offset==0, got %+v visible=%v", out.Cursor, out.CursorVisible)
	}

	out = BuildScrollbackViewFromTerminal(8, 3, scrollback, live, 1, 0)
	if out.Cursor == nil || out.Cursor.X != 0 || out.Cursor.Y != 0 || !out.CursorVisible {
		t.Fatalf("expected anchored visible cursor at 0,0 when offset==1, got %+v visible=%v", out.Cursor, out.CursorVisible)
	}
}

func TestBuildScrollbackViewFromProtoSupportsHorizontalPan(t *testing.T) {
	live := makeLiveSnapshot(12, 1)
	writeSnapshotRow(live, 0, "0123456789ab")
	out := BuildScrollbackViewFromProto(4, 1, nil, live, 0, 6)
	if got := rowStringFromSnapshot(out, 0); got != "6789" {
		t.Fatalf("expected horizontally panned live row, got %q", got)
	}
}

func TestBuildScrollbackViewFromTerminalNormalizesTrailingStyledSpaces(t *testing.T) {
	live := makeLiveSnapshot(6, 1)
	rows := []terminal.ScrollbackRow{
		{
			Cells: []terminal.Cell{
				{Rune: 'a'},
				{Rune: ' ', BG: terminal.ColorIndexed | 5},
				{Rune: 'b'},
				{Rune: 'c'},
				{Rune: ' ', FG: terminal.ColorIndexed | 1},
				{Rune: ' ', Mode: terminal.ModeBold, BG: terminal.ColorIndexed | 2},
			},
		},
	}

	out := BuildScrollbackViewFromTerminal(6, 1, rows, live, 1, 0)
	if got := rowStringFromSnapshot(out, 0); got != "a bc  " {
		t.Fatalf("expected scrollback row content preserved, got %q", got)
	}

	if out.Bg[1] != terminal.ColorIndexed|5 {
		t.Fatalf("expected interior styled space preserved, got bg=%#x", out.Bg[1])
	}
	for _, col := range []int{4, 5} {
		if out.Modes[col] != 0 || out.Fg[col] != terminal.ColorDefault || out.Bg[col] != terminal.ColorDefault {
			t.Fatalf("expected trailing space attrs cleared at col=%d, got mode=%d fg=%#x bg=%#x", col, out.Modes[col], out.Fg[col], out.Bg[col])
		}
	}
}

func TestBuildScrollbackViewFromTerminalNormalizesTrailingStyledNULCells(t *testing.T) {
	live := makeLiveSnapshot(6, 1)
	rows := []terminal.ScrollbackRow{
		{
			Cells: []terminal.Cell{
				{Rune: 'a'},
				{Rune: 0, BG: terminal.ColorIndexed | 6}, // interior blank: must remain
				{Rune: 'b'},
				{Rune: 0, FG: terminal.ColorIndexed | 1},   // trailing blank: must clear
				{Rune: 0, Mode: terminal.ModeUnderline},    // trailing blank: must clear
				{Rune: ' ', BG: terminal.ColorIndexed | 2}, // trailing blank: must clear
			},
		},
	}

	out := BuildScrollbackViewFromTerminal(6, 1, rows, live, 1, 0)
	if got := rowStringFromSnapshot(out, 0); got != "a b   " {
		t.Fatalf("expected scrollback row content preserved, got %q", got)
	}

	if out.Bg[1] != terminal.ColorIndexed|6 {
		t.Fatalf("expected interior NUL-blank attrs preserved, got bg=%#x", out.Bg[1])
	}
	for _, col := range []int{3, 4, 5} {
		if out.Modes[col] != 0 || out.Fg[col] != terminal.ColorDefault || out.Bg[col] != terminal.ColorDefault {
			t.Fatalf("expected trailing blank attrs cleared at col=%d, got mode=%d fg=%#x bg=%#x", col, out.Modes[col], out.Fg[col], out.Bg[col])
		}
	}
}

func TestBuildScrollbackViewFromProtoNormalizesTrailingStyledSpaces(t *testing.T) {
	live := makeLiveSnapshot(6, 1)
	rows := []*protocolpb.ScrollbackRow{
		{
			Runes: []uint32{'a', ' ', 'b', 'c', ' ', ' '},
			Modes: []int32{0, 0, 0, 0, 0, int32(terminal.ModeUnderline)},
			Fg: []uint32{
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorIndexed | 1,
				terminal.ColorDefault,
			},
			Bg: []uint32{
				terminal.ColorDefault,
				terminal.ColorIndexed | 5,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorIndexed | 2,
			},
		},
	}

	out := BuildScrollbackViewFromProto(6, 1, rows, live, 1, 0)
	if got := rowStringFromSnapshot(out, 0); got != "a bc  " {
		t.Fatalf("expected scrollback row content preserved, got %q", got)
	}

	if out.Bg[1] != terminal.ColorIndexed|5 {
		t.Fatalf("expected interior styled space preserved, got bg=%#x", out.Bg[1])
	}
	for _, col := range []int{4, 5} {
		if out.Modes[col] != 0 || out.Fg[col] != terminal.ColorDefault || out.Bg[col] != terminal.ColorDefault {
			t.Fatalf("expected trailing space attrs cleared at col=%d, got mode=%d fg=%#x bg=%#x", col, out.Modes[col], out.Fg[col], out.Bg[col])
		}
	}
}

func TestBuildScrollbackViewFromProtoNormalizesTrailingStyledNULCells(t *testing.T) {
	live := makeLiveSnapshot(6, 1)
	rows := []*protocolpb.ScrollbackRow{
		{
			Runes: []uint32{'a', 0, 'b', 0, 0, ' '},
			Modes: []int32{0, 0, 0, 0, int32(terminal.ModeUnderline), 0},
			Fg: []uint32{
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorIndexed | 1,
				terminal.ColorDefault,
				terminal.ColorDefault,
			},
			Bg: []uint32{
				terminal.ColorDefault,
				terminal.ColorIndexed | 6,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorDefault,
				terminal.ColorIndexed | 2,
			},
		},
	}

	out := BuildScrollbackViewFromProto(6, 1, rows, live, 1, 0)
	if got := rowStringFromSnapshot(out, 0); got != "a b   " {
		t.Fatalf("expected scrollback row content preserved, got %q", got)
	}

	if out.Bg[1] != terminal.ColorIndexed|6 {
		t.Fatalf("expected interior NUL-blank attrs preserved, got bg=%#x", out.Bg[1])
	}
	for _, col := range []int{3, 4, 5} {
		if out.Modes[col] != 0 || out.Fg[col] != terminal.ColorDefault || out.Bg[col] != terminal.ColorDefault {
			t.Fatalf("expected trailing blank attrs cleared at col=%d, got mode=%d fg=%#x bg=%#x", col, out.Modes[col], out.Fg[col], out.Bg[col])
		}
	}
}

func TestBuildScrollbackViewHandlesNilLive(t *testing.T) {
	out := BuildScrollbackViewFromProto(10, 4, nil, nil, 0, 0)
	if out == nil {
		t.Fatalf("expected empty snapshot")
	}
	if out.Cols != 10 || out.Rows != 4 {
		t.Fatalf("unexpected nil-live size: %dx%d", out.Cols, out.Rows)
	}
}

func rowStringFromSnapshot(snap *protocolpb.Snapshot, row int) string {
	if snap == nil || row < 0 || row >= int(snap.Rows) {
		return ""
	}
	cols := int(snap.Cols)
	start := row * cols
	out := make([]rune, 0, cols)
	for i := 0; i < cols; i++ {
		r := rune(snap.Runes[start+i])
		if r == 0 {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}
