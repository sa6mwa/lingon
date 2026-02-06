package emu

import (
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestGraphemeClustersDoNotAdvanceCursorTwice(t *testing.T) {
	e := New(10, 2)
	if err := e.Write([]byte("e\u0301")); err != nil { // e + combining acute
		t.Fatalf("write combining: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Cursor.X != 1 {
		t.Fatalf("expected cursor x=1, got %d", snap.Cursor.X)
	}
	if len(snap.Cells) == 0 || snap.Cells[0].Grapheme != "e\u0301" {
		t.Fatalf("expected grapheme stored for combining, got %+v", snap.Cells[0])
	}
}

func TestEmojiVariationSelectorClusterWidth(t *testing.T) {
	e := New(10, 2)
	cluster := "❌️"
	if err := e.Write([]byte(cluster)); err != nil {
		t.Fatalf("write cluster: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	want := runewidth.StringWidth(cluster)
	if want <= 0 {
		want = 1
	}
	if snap.Cursor.X != want {
		t.Fatalf("expected cursor x=%d, got %d (cell=%+v)", want, snap.Cursor.X, snap.Cells[0])
	}
	if len(snap.Cells) == 0 || snap.Cells[0].Grapheme != cluster {
		t.Fatalf("expected grapheme stored for cluster, got %+v", snap.Cells[0])
	}
}

func TestZWJClusterStaysInSingleCell(t *testing.T) {
	e := New(20, 2)
	cluster := "👨‍👩‍👧‍👦"
	if err := e.Write([]byte(cluster)); err != nil {
		t.Fatalf("write cluster: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	want := runewidth.StringWidth(cluster)
	if want <= 0 {
		want = 1
	}
	if snap.Cursor.X != want {
		t.Fatalf("expected cursor x=%d, got %d (cell=%+v)", want, snap.Cursor.X, snap.Cells[0])
	}
	if len(snap.Cells) == 0 || snap.Cells[0].Grapheme != cluster {
		t.Fatalf("expected grapheme stored for cluster, got %+v", snap.Cells[0])
	}
}
