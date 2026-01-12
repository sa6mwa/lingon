package host

import (
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestSnapshotToProto(t *testing.T) {
	snap := terminal.Snapshot{
		Cols:          2,
		Rows:          1,
		Cursor:        terminal.Cursor{X: 1, Y: 0},
		CursorVisible: true,
		Mode:          1,
		Title:         "test",
		Cells: []terminal.Cell{
			{Rune: 'a', Mode: 1, FG: 2, BG: 3},
			{Rune: 'b', Mode: 4, FG: 5, BG: 6},
		},
	}
	proto := snapshotToProto(snap)
	if proto.Cols != 2 || proto.Rows != 1 {
		t.Fatalf("size = %dx%d", proto.Cols, proto.Rows)
	}
	if len(proto.Runes) != 2 || len(proto.Modes) != 2 {
		t.Fatalf("expected 2 cells")
	}
	if proto.Runes[0] != uint32('a') || proto.Runes[1] != uint32('b') {
		t.Fatalf("runes mismatch")
	}
	if proto.Cursor.X != 1 || proto.Cursor.Y != 0 {
		t.Fatalf("cursor mismatch")
	}
}
