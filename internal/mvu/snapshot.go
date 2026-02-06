package mvu

import "pkt.systems/lingon/internal/protocolpb"

// BlankSnapshot allocates an empty snapshot of the requested size.
func BlankSnapshot(cols, rows int) *protocolpb.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	size := cols * rows
	return &protocolpb.Snapshot{
		Cols:  uint32(cols),
		Rows:  uint32(rows),
		Runes: make([]uint32, size),
		Modes: make([]int32, size),
		Fg:    make([]uint32, size),
		Bg:    make([]uint32, size),
	}
}
