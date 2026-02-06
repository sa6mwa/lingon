package render

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/grapheme"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestGraphemeSamplesRoundTrip(t *testing.T) {
	const cols = 80
	const rows = 10

	for _, sample := range grapheme.Samples() {
		t.Run(sample.Name, func(t *testing.T) {
			input := strings.Join(sample.Lines, "\n") + "\n"
			src := emu.New(cols, rows)
			if err := src.Write([]byte(input)); err != nil {
				t.Fatalf("emu write: %v", err)
			}
			snap, err := src.Snapshot()
			if err != nil {
				t.Fatalf("emu snapshot: %v", err)
			}

			protoSnap := protocol.SnapshotToProto(snap)
			var buf bytes.Buffer
			if err := SnapshotViewport(&buf, protoSnap, cols, rows); err != nil {
				t.Fatalf("render snapshot: %v", err)
			}

			dst := emu.New(cols, rows)
			if err := dst.Write(buf.Bytes()); err != nil {
				t.Fatalf("emu write roundtrip: %v", err)
			}
			round, err := dst.Snapshot()
			if err != nil {
				t.Fatalf("emu snapshot roundtrip: %v", err)
			}

			compareSnapshots(t, snap, round)
		})
	}
}

func compareSnapshots(t *testing.T, a, b terminal.Snapshot) {
	t.Helper()
	if a.Cols != b.Cols || a.Rows != b.Rows {
		t.Fatalf("size mismatch: %dx%d vs %dx%d", a.Cols, a.Rows, b.Cols, b.Rows)
	}
	if a.Cursor != b.Cursor || a.CursorVisible != b.CursorVisible {
		t.Fatalf("cursor mismatch: %+v/%v vs %+v/%v", a.Cursor, a.CursorVisible, b.Cursor, b.CursorVisible)
	}
	if len(a.Cells) != len(b.Cells) {
		t.Fatalf("cell count mismatch: %d vs %d", len(a.Cells), len(b.Cells))
	}
	for i := range a.Cells {
		ac := a.Cells[i]
		bc := b.Cells[i]
		if ac.Rune != bc.Rune || ac.Grapheme != bc.Grapheme || ac.Mode != bc.Mode || ac.FG != bc.FG || ac.BG != bc.BG {
			t.Fatalf("cell %d mismatch: %+v vs %+v", i, ac, bc)
		}
	}
}
