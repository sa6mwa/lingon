package mvu

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
)

// CursorFromSnapshot resolves viewport cursor coordinates and visibility.
func CursorFromSnapshot(snap *protocolpb.Snapshot, cols, rows int) Cursor {
	if snap == nil {
		return Cursor{Row: 1, Col: 1, Visible: false}
	}
	if cols <= 0 {
		cols = int(snap.Cols)
	}
	if rows <= 0 {
		rows = int(snap.Rows)
	}
	row, col, inView := render.ViewportCursorPosition(snap, cols, rows)
	return Cursor{
		Row:     row,
		Col:     col,
		Visible: inView && snap.CursorVisible,
	}
}
