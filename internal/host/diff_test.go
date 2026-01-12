package host

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestDiffSnapshots(t *testing.T) {
	prev := snapshotForTest(2, 1, []rune{'a', 'b'})
	next := snapshotForTest(2, 1, []rune{'a', 'c'})

	diff, full := diffSnapshots(prev, next)
	if full {
		t.Fatalf("expected diff, got full snapshot")
	}
	if diff == nil {
		t.Fatalf("expected diff")
	}
	if len(diff.DiffRows) != 1 {
		t.Fatalf("expected 1 diff row, got %d", len(diff.DiffRows))
	}
	if diff.DiffRows[0].Row != 0 {
		t.Fatalf("row = %d, want 0", diff.DiffRows[0].Row)
	}
	if len(diff.DiffRows[0].Runes) != 2 {
		t.Fatalf("expected runes length 2")
	}
	if diff.DiffRows[0].Runes[1] != uint32('c') {
		t.Fatalf("expected updated rune")
	}
}

func snapshotForTest(cols, rows int, runes []rune) *protocolpb.Snapshot {
	snap := &protocolpb.Snapshot{
		Cols:  uint32(cols),
		Rows:  uint32(rows),
		Runes: make([]uint32, cols*rows),
		Modes: make([]int32, cols*rows),
		Fg:    make([]uint32, cols*rows),
		Bg:    make([]uint32, cols*rows),
	}
	for i := range runes {
		if i >= len(snap.Runes) {
			break
		}
		snap.Runes[i] = uint32(runes[i])
	}
	return snap
}
