package emu

import "testing"

func TestInlineOriginOffsetsCUP(t *testing.T) {
	e := New(20, 10)
	e.SetInlineOriginRow(5)
	if err := e.Write([]byte("\x1b[1;1H")); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Cursor.Y != 4 || snap.Cursor.X != 0 {
		t.Fatalf("expected cursor at row 5 col 1, got row %d col %d", snap.Cursor.Y+1, snap.Cursor.X+1)
	}
}

func TestInlineOriginOffsetsScrollRegion(t *testing.T) {
	e := New(20, 10)
	e.SetInlineOriginRow(3)
	if err := e.Write([]byte("\x1b[1;5r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if e.scr.scrollTop != 2 || e.scr.scrollBottom != 6 {
		t.Fatalf("expected scroll region 3..7, got %d..%d", e.scr.scrollTop+1, e.scr.scrollBottom+1)
	}
}
