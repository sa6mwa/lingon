package session

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/theme"
)

func TestRenderSnapshotAvoidsClearWhenNotForced(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols:          4,
		Rows:          1,
		Runes:         []uint32{'a', 'b', 'c', 'd'},
		Modes:         []int32{0, 0, 0, 0},
		Fg:            []uint32{0, 0, 0, 0},
		Bg:            []uint32{0, 0, 0, 0},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	snap := &protocolpb.Snapshot{
		Cols:          4,
		Rows:          1,
		Runes:         []uint32{'a', 'b', 'x', 'd'},
		Modes:         []int32{0, 0, 0, 0},
		Fg:            []uint32{0, 0, 0, 0},
		Bg:            []uint32{0, 0, 0, 0},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := renderSnapshot(&buf, prev, snap, 4, 1, false); err != nil {
		t.Fatalf("renderSnapshot not forced: %v", err)
	}
	if strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("unexpected clear screen when not forced and size unchanged")
	}

	buf.Reset()
	if err := renderSnapshot(&buf, prev, snap, 4, 1, true); err != nil {
		t.Fatalf("renderSnapshot forced: %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("expected clear screen when forced")
	}
}

func TestRenderSnapshotClearsOnResizeWhenForced(t *testing.T) {
	prev := &protocolpb.Snapshot{
		Cols:          3,
		Rows:          1,
		Runes:         []uint32{'a', 'b', 'c'},
		Modes:         []int32{0, 0, 0},
		Fg:            []uint32{0, 0, 0},
		Bg:            []uint32{0, 0, 0},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	snap := &protocolpb.Snapshot{
		Cols:          4,
		Rows:          1,
		Runes:         []uint32{'a', 'b', 'c', 'd'},
		Modes:         []int32{0, 0, 0, 0},
		Fg:            []uint32{0, 0, 0, 0},
		Bg:            []uint32{0, 0, 0, 0},
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}

	var buf bytes.Buffer
	if err := renderSnapshot(&buf, prev, snap, 4, 1, true); err != nil {
		t.Fatalf("renderSnapshot forced resize: %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("expected clear screen when size changes")
	}
}

func renderSnapshot(w io.Writer, prev, snap *protocolpb.Snapshot, cols, rows int, force bool) error {
	out, err := mvu.RenderHost(mvu.HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       mvu.Cursor{Row: 1, Col: 1, Visible: true},
		State: mvu.State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: force,
	})
	if err != nil {
		return err
	}
	_, err = w.Write(out.Bytes)
	return err
}
