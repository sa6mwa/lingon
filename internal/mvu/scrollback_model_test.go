package mvu

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestScrollbackViewportPagingAndBounds(t *testing.T) {
	var view ScrollbackViewport
	view.Enter()
	if !view.Active() {
		t.Fatalf("expected active after Enter")
	}
	if !view.Page(200, 20, 1, 10) {
		t.Fatalf("expected page up to change offset")
	}
	if got := view.Offset(); got != 10 {
		t.Fatalf("expected offset=10, got %d", got)
	}
	if !view.Page(200, 20, 1, 0) {
		t.Fatalf("expected default step page to change offset")
	}
	if got := view.Offset(); got != 30 {
		t.Fatalf("expected offset=30, got %d", got)
	}
	if !view.Bottom() {
		t.Fatalf("expected bottom to reset non-zero offset")
	}
	if got := view.Offset(); got != 0 {
		t.Fatalf("expected offset reset to zero, got %d", got)
	}
	if !view.Top(200, 20) {
		t.Fatalf("expected top to jump to max offset")
	}
	if got := view.Offset(); got != 180 {
		t.Fatalf("expected max offset=180, got %d", got)
	}
	view.Normalize(50, 20)
	if got := view.Offset(); got != 30 {
		t.Fatalf("expected normalized offset=30, got %d", got)
	}
	view.Exit()
	if view.Active() || view.Offset() != 0 {
		t.Fatalf("expected exit to clear scrollback view state")
	}
}

func TestScrollbackViewportPercent(t *testing.T) {
	var view ScrollbackViewport
	view.SetOffset(200, 20, 90)
	if got := view.Percent(200, 20); got <= 0 || got >= 100 {
		t.Fatalf("expected middle percentage for offset 90, got %d", got)
	}
	view.Bottom()
	if got := view.Percent(200, 20); got != 100 {
		t.Fatalf("expected bottom percentage=100, got %d", got)
	}
}

func TestProtoScrollbackBufferApplyTrimAndClone(t *testing.T) {
	b := NewProtoScrollbackBuffer(2)
	b.Apply(&protocolpb.Scrollback{
		Cols: 80,
		Rows: []*protocolpb.ScrollbackRow{
			{Runes: []uint32{'a'}},
			{Runes: []uint32{'b'}},
			{Runes: []uint32{'c'}},
		},
	})
	if got := b.Len(); got != 2 {
		t.Fatalf("expected trimmed rows=2, got %d", got)
	}
	rows := b.Rows()
	if len(rows) != 2 || rows[0].Runes[0] != 'b' || rows[1].Runes[0] != 'c' {
		t.Fatalf("unexpected rows after trim: %#v", rows)
	}
	rows[0].Runes[0] = 'x'
	rows2 := b.Rows()
	if rows2[0].Runes[0] != 'b' {
		t.Fatalf("expected deep-cloned rows")
	}
}

func TestProtoScrollbackBufferClearsOnWidthChangeAndClearFlag(t *testing.T) {
	b := NewProtoScrollbackBuffer(10)
	b.Apply(&protocolpb.Scrollback{
		Cols: 80,
		Rows: []*protocolpb.ScrollbackRow{
			{Runes: []uint32{'a'}},
			{Runes: []uint32{'b'}},
		},
	})
	if got := b.Len(); got != 2 {
		t.Fatalf("expected initial rows=2, got %d", got)
	}
	b.Apply(&protocolpb.Scrollback{
		Cols: 100,
		Rows: []*protocolpb.ScrollbackRow{
			{Runes: []uint32{'z'}},
		},
	})
	if got := b.Len(); got != 1 {
		t.Fatalf("expected width change reset + append, got len=%d", got)
	}
	b.Apply(&protocolpb.Scrollback{Clear: true})
	if got := b.Len(); got != 0 {
		t.Fatalf("expected clear flag to wipe rows, got len=%d", got)
	}
}
