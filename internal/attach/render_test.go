package attach

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func TestRenderSnapshot(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:          3,
		Rows:          1,
		Runes:         []uint32{'a', 'b', 'c'},
		Cursor:        &protocolpb.Cursor{X: 1, Y: 0},
		CursorVisible: true,
		Title:         "title",
	}
	var buf bytes.Buffer
	if err := RenderSnapshot(&buf, snap); err != nil {
		t.Fatalf("RenderSnapshot: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "abc") {
		t.Fatalf("expected output to contain row")
	}
	if !strings.Contains(out, "\x1b[1;2H") {
		t.Fatalf("expected cursor move")
	}
}

func TestRenderSnapshotViewportCrop(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:          4,
		Rows:          2,
		Runes:         []uint32{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'},
		Cursor:        &protocolpb.Cursor{X: 3, Y: 1},
		CursorVisible: true,
	}
	var buf bytes.Buffer
	if err := RenderSnapshotViewport(&buf, snap, 2, 1); err != nil {
		t.Fatalf("RenderSnapshotViewport: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "gh") {
		t.Fatalf("expected cropped row")
	}
	if !strings.Contains(out, "\x1b[1;2H") {
		t.Fatalf("expected cursor move")
	}
}

func TestRenderSnapshotViewportPad(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:          2,
		Rows:          1,
		Runes:         []uint32{'x', 'y'},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	var buf bytes.Buffer
	if err := RenderSnapshotViewport(&buf, snap, 4, 2); err != nil {
		t.Fatalf("RenderSnapshotViewport: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "xy  ") {
		t.Fatalf("expected padded row")
	}
}

func TestRenderSnapshotColors(t *testing.T) {
	snap := &protocolpb.Snapshot{
		Cols:  1,
		Rows:  1,
		Runes: []uint32{'A'},
		Modes: []int32{0},
		Fg:    []uint32{terminal.ColorIndexed | 1},
		Bg:    []uint32{terminal.ColorIndexed | 2},
	}
	var buf bytes.Buffer
	if err := RenderSnapshot(&buf, snap); err != nil {
		t.Fatalf("RenderSnapshot: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "31") {
		t.Fatalf("expected fg color sgr")
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("expected bg color sgr")
	}
}
