package session

import (
	"testing"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func TestCursorQueryPositionIgnoresTopOverlay(t *testing.T) {
	r := &Runner{}

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Cursor:        &protocolpb.Cursor{X: 4, Y: 0},
		CursorVisible: true,
		Modes:         make([]int32, 80*24),
	}

	row, col, ok := r.cursorQueryPosition(snap, 80, 24)
	if !ok {
		t.Fatalf("expected cursor position")
	}
	if row != 1 || col != 5 {
		t.Fatalf("expected row=1 col=5, got row=%d col=%d", row, col)
	}
}

func TestCursorQueryPositionKeepsRowWithoutOverlay(t *testing.T) {
	r := &Runner{}

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Cursor:        &protocolpb.Cursor{X: 4, Y: 0},
		CursorVisible: true,
		Modes:         make([]int32, 80*24),
	}

	row, col, ok := r.cursorQueryPosition(snap, 80, 24)
	if !ok {
		t.Fatalf("expected cursor position")
	}
	if row != 1 || col != 5 {
		t.Fatalf("expected row=1 col=5, got row=%d col=%d", row, col)
	}
}

func TestCursorQueryFuncUsesRenderedCursor(t *testing.T) {
	r := &Runner{}
	r.renderCursorMu.Lock()
	r.renderCursorRow = 3
	r.renderCursorCol = 7
	r.renderCursorVisible = true
	r.renderCursorMu.Unlock()

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("pty open: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 40, Rows: 10}); err != nil {
		t.Fatalf("setsize: %v", err)
	}
	query := r.cursorQueryFunc(ptmx, ptmx)
	row, col, ok := query(terminal.Snapshot{Cols: 80, Rows: 24})
	if !ok {
		t.Fatalf("expected cursor position")
	}
	if row != 3 || col != 7 {
		t.Fatalf("expected row=3 col=7, got row=%d col=%d", row, col)
	}
}

func TestCursorQueryFuncUsesRawCursorWhenFullScreen(t *testing.T) {
	r := &Runner{}
	r.renderCursorMu.Lock()
	r.renderCursorRow = 3
	r.renderCursorCol = 7
	r.renderCursorVisible = true
	r.renderCursorMu.Unlock()

	query := r.cursorQueryFunc(nil, nil)
	row, col, ok := query(terminal.Snapshot{
		Cols:   17,
		Rows:   9,
		Cursor: terminal.Cursor{X: 4, Y: 1},
	})
	if !ok {
		t.Fatalf("expected cursor position")
	}
	if row != 2 || col != 5 {
		t.Fatalf("expected row=2 col=5, got row=%d col=%d", row, col)
	}
}
