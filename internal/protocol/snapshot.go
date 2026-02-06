package protocol

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// SnapshotToProto converts a terminal snapshot into a protocol snapshot.
func SnapshotToProto(s terminal.Snapshot) *protocolpb.Snapshot {
	runes := make([]uint32, 0, len(s.Cells))
	modes := make([]int32, 0, len(s.Cells))
	fg := make([]uint32, 0, len(s.Cells))
	bg := make([]uint32, 0, len(s.Cells))
	graphemes := make([]string, len(s.Cells))
	hasGraphemes := false

	for i, cell := range s.Cells {
		runes = append(runes, uint32(cell.Rune))
		modes = append(modes, int32(cell.Mode))
		fg = append(fg, cell.FG)
		bg = append(bg, cell.BG)
		if cell.Grapheme != "" {
			graphemes[i] = cell.Grapheme
			hasGraphemes = true
		}
	}
	if !hasGraphemes {
		graphemes = nil
	}

	return &protocolpb.Snapshot{
		Cols:          uint32(s.Cols),
		Rows:          uint32(s.Rows),
		Runes:         runes,
		Modes:         modes,
		Fg:            fg,
		Bg:            bg,
		Cursor:        &protocolpb.Cursor{X: uint32(s.Cursor.X), Y: uint32(s.Cursor.Y)},
		CursorVisible: s.CursorVisible,
		Mode:          s.Mode,
		Title:         s.Title,
		Graphemes:     graphemes,
	}
}
