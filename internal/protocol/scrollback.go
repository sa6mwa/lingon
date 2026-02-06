package protocol

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// ScrollbackRowToProto converts a terminal scrollback row into a protocol row.
func ScrollbackRowToProto(row terminal.ScrollbackRow) *protocolpb.ScrollbackRow {
	runes := make([]uint32, 0, len(row.Cells))
	modes := make([]int32, 0, len(row.Cells))
	fg := make([]uint32, 0, len(row.Cells))
	bg := make([]uint32, 0, len(row.Cells))
	graphemes := make([]string, len(row.Cells))
	hasGraphemes := false

	for i, cell := range row.Cells {
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

	return &protocolpb.ScrollbackRow{
		Runes:     runes,
		Modes:     modes,
		Fg:        fg,
		Bg:        bg,
		Graphemes: graphemes,
	}
}
