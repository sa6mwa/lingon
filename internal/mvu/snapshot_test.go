package mvu

import "testing"

func TestBlankSnapshotAllocatesBySize(t *testing.T) {
	snap := BlankSnapshot(7, 3)
	if snap.Cols != 7 || snap.Rows != 3 {
		t.Fatalf("unexpected snapshot size: cols=%d rows=%d", snap.Cols, snap.Rows)
	}
	if len(snap.Runes) != 21 || len(snap.Modes) != 21 || len(snap.Fg) != 21 || len(snap.Bg) != 21 {
		t.Fatalf("unexpected backing lengths: runes=%d modes=%d fg=%d bg=%d", len(snap.Runes), len(snap.Modes), len(snap.Fg), len(snap.Bg))
	}
}

func TestBlankSnapshotClampsInvalidDimensions(t *testing.T) {
	snap := BlankSnapshot(0, -2)
	if snap.Cols != 1 || snap.Rows != 1 {
		t.Fatalf("expected invalid dimensions to clamp to 1x1; got %dx%d", snap.Cols, snap.Rows)
	}
	if len(snap.Runes) != 1 {
		t.Fatalf("expected 1-cell backing slice, got %d", len(snap.Runes))
	}
}
