package emu

import "testing"

func TestPrivateCSIUDoesNotRestoreCursor(t *testing.T) {
	e := New(10, 5)
	if err := e.Write([]byte("\x1b[2;2H")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := e.Write([]byte("\x1b[s")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := e.Write([]byte("\x1b[3;3H")); err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := e.Write([]byte("\x1b[>7u")); err != nil {
		t.Fatalf("private restore: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Cursor.Y != 2 || snap.Cursor.X != 2 {
		t.Fatalf("expected cursor to remain at row 3 col 3, got row %d col %d", snap.Cursor.Y+1, snap.Cursor.X+1)
	}
	if err := e.Write([]byte("\x1b[<1u")); err != nil {
		t.Fatalf("private restore (<): %v", err)
	}
	snap, err = e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Cursor.Y != 2 || snap.Cursor.X != 2 {
		t.Fatalf("expected cursor to remain at row 3 col 3 after <, got row %d col %d", snap.Cursor.Y+1, snap.Cursor.X+1)
	}
	if err := e.Write([]byte("\x1b[u")); err != nil {
		t.Fatalf("restore: %v", err)
	}
	snap, err = e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Cursor.Y != 1 || snap.Cursor.X != 1 {
		t.Fatalf("expected cursor to restore to row 2 col 2, got row %d col %d", snap.Cursor.Y+1, snap.Cursor.X+1)
	}
}
