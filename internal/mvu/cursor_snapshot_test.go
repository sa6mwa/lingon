package mvu

import "testing"

func TestCursorFromSnapshotDefaultsAndVisibility(t *testing.T) {
	if cur := CursorFromSnapshot(nil, 0, 0); cur.Row != 1 || cur.Col != 1 || cur.Visible {
		t.Fatalf("unexpected nil-snapshot cursor: %+v", cur)
	}

	snap := makeSnapshot(10, 4, 3, 1)
	snap.CursorVisible = true
	cur := CursorFromSnapshot(snap, 10, 4)
	if cur.Row != 2 || cur.Col != 4 {
		t.Fatalf("unexpected cursor position: %+v", cur)
	}
	if !cur.Visible {
		t.Fatalf("expected cursor visible")
	}
}
