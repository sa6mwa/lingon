package mvu

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// BuildScrollbackViewFromTerminal composes a snapshot view that includes
// terminal scrollback rows followed by live rows at the current offset.
func BuildScrollbackViewFromTerminal(cols, viewRows int, scrollback []terminal.ScrollbackRow, live *protocolpb.Snapshot, offset int) *protocolpb.Snapshot {
	if live == nil {
		return &protocolpb.Snapshot{Cols: uint32(cols), Rows: uint32(viewRows)}
	}
	cols, viewRows, start, _ := normalizeScrollbackBounds(cols, viewRows, len(scrollback), live, offset)
	size := cols * viewRows
	runes := make([]uint32, size)
	modes := make([]int32, size)
	fg := make([]uint32, size)
	bg := make([]uint32, size)
	var graphemes []string
	hasGraphemes := false
	if len(live.Graphemes) > 0 {
		graphemes = make([]string, size)
	}

	fillRowFromCells := func(dstRow int, cells []terminal.Cell) {
		if len(cells) == 0 {
			return
		}
		startIdx := dstRow * cols
		for x := 0; x < cols && x < len(cells); x++ {
			idx := startIdx + x
			cell := cells[x]
			runes[idx] = uint32(cell.Rune)
			modes[idx] = int32(cell.Mode)
			fg[idx] = cell.FG
			bg[idx] = cell.BG
			if graphemes != nil && cell.Grapheme != "" {
				graphemes[idx] = cell.Grapheme
				hasGraphemes = true
			}
		}
	}

	for viewRow := 0; viewRow < viewRows; viewRow++ {
		sourceRow := start + viewRow
		if sourceRow < len(scrollback) {
			fillRowFromCells(viewRow, scrollback[sourceRow].Cells)
		} else {
			liveRow := sourceRow - len(scrollback)
			if liveRow >= 0 && liveRow < int(live.Rows) {
				rowStart := liveRow * int(live.Cols)
				for x := 0; x < cols; x++ {
					idx := rowStart + x
					dst := viewRow*cols + x
					if idx < len(live.Runes) {
						runes[dst] = live.Runes[idx]
					}
					if idx < len(live.Modes) {
						modes[dst] = live.Modes[idx]
					}
					if idx < len(live.Fg) {
						fg[dst] = live.Fg[idx]
					}
					if idx < len(live.Bg) {
						bg[dst] = live.Bg[idx]
					}
					if graphemes != nil && idx < len(live.Graphemes) {
						graphemes[dst] = live.Graphemes[idx]
						if live.Graphemes[idx] != "" {
							hasGraphemes = true
						}
					}
				}
			}
		}
		normalizeTrailingBlankAttrs(viewRow, cols, runes, modes, fg, bg, graphemes)
	}
	return finalizeScrollbackView(cols, viewRows, start, len(scrollback), live, runes, modes, fg, bg, graphemes, hasGraphemes)
}

// BuildScrollbackViewFromProto composes a snapshot view that includes protocol
// scrollback rows followed by live rows at the current offset.
func BuildScrollbackViewFromProto(cols, viewRows int, scrollback []*protocolpb.ScrollbackRow, live *protocolpb.Snapshot, offset int) *protocolpb.Snapshot {
	if live == nil {
		return &protocolpb.Snapshot{Cols: uint32(cols), Rows: uint32(viewRows)}
	}
	cols, viewRows, start, _ := normalizeScrollbackBounds(cols, viewRows, len(scrollback), live, offset)
	size := cols * viewRows
	runes := make([]uint32, size)
	modes := make([]int32, size)
	fg := make([]uint32, size)
	bg := make([]uint32, size)
	var graphemes []string
	hasGraphemes := false
	if len(live.Graphemes) > 0 {
		graphemes = make([]string, size)
	}

	fillProtoRow := func(dstRow int, row *protocolpb.ScrollbackRow) {
		if row == nil {
			return
		}
		startIdx := dstRow * cols
		for x := 0; x < cols; x++ {
			idx := startIdx + x
			if x < len(row.Runes) {
				runes[idx] = row.Runes[x]
			}
			if x < len(row.Modes) {
				modes[idx] = row.Modes[x]
			}
			if x < len(row.Fg) {
				fg[idx] = row.Fg[x]
			}
			if x < len(row.Bg) {
				bg[idx] = row.Bg[x]
			}
			if graphemes != nil && x < len(row.Graphemes) {
				graphemes[idx] = row.Graphemes[x]
				if row.Graphemes[x] != "" {
					hasGraphemes = true
				}
			}
		}
	}

	for viewRow := 0; viewRow < viewRows; viewRow++ {
		sourceRow := start + viewRow
		if sourceRow < len(scrollback) {
			fillProtoRow(viewRow, scrollback[sourceRow])
		} else {
			liveRow := sourceRow - len(scrollback)
			if liveRow >= 0 && liveRow < int(live.Rows) {
				rowStart := liveRow * int(live.Cols)
				for x := 0; x < cols; x++ {
					idx := rowStart + x
					dst := viewRow*cols + x
					if idx < len(live.Runes) {
						runes[dst] = live.Runes[idx]
					}
					if idx < len(live.Modes) {
						modes[dst] = live.Modes[idx]
					}
					if idx < len(live.Fg) {
						fg[dst] = live.Fg[idx]
					}
					if idx < len(live.Bg) {
						bg[dst] = live.Bg[idx]
					}
					if graphemes != nil && idx < len(live.Graphemes) {
						graphemes[dst] = live.Graphemes[idx]
						if live.Graphemes[idx] != "" {
							hasGraphemes = true
						}
					}
				}
			}
		}
		normalizeTrailingBlankAttrs(viewRow, cols, runes, modes, fg, bg, graphemes)
	}
	return finalizeScrollbackView(cols, viewRows, start, len(scrollback), live, runes, modes, fg, bg, graphemes, hasGraphemes)
}

func normalizeTrailingBlankAttrs(row, cols int, runes []uint32, modes []int32, fg []uint32, bg []uint32, graphemes []string) {
	rowStart := row * cols
	rowEnd := rowStart + cols - 1
	for idx := rowEnd; idx >= rowStart; idx-- {
		if graphemes != nil && idx < len(graphemes) && graphemes[idx] != "" {
			break
		}
		r := uint32(0)
		if idx < len(runes) {
			r = runes[idx]
		}
		// Trailing blanks can be represented as either NUL or SPACE cells.
		// Normalize both to default attrs so copy/select does not include
		// style-extended trailing whitespace in scrollbuffer mode.
		if r != 0 && r != uint32(' ') {
			break
		}
		if idx < len(modes) {
			modes[idx] = 0
		}
		if idx < len(fg) {
			fg[idx] = terminal.ColorDefault
		}
		if idx < len(bg) {
			bg[idx] = terminal.ColorDefault
		}
	}
}

func normalizeScrollbackBounds(cols, viewRows, scrollbackCount int, live *protocolpb.Snapshot, offset int) (int, int, int, int) {
	if cols <= 0 {
		cols = int(live.Cols)
	}
	if viewRows <= 0 {
		viewRows = int(live.Rows)
	}
	totalRows := scrollbackCount + int(live.Rows)
	maxOffset := totalRows - viewRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	start := totalRows - viewRows - offset
	if start < 0 {
		start = 0
	}
	return cols, viewRows, start, offset
}

func finalizeScrollbackView(cols, viewRows, start, scrollbackCount int, live *protocolpb.Snapshot, runes []uint32, modes []int32, fg []uint32, bg []uint32, graphemes []string, hasGraphemes bool) *protocolpb.Snapshot {
	if !hasGraphemes {
		graphemes = nil
	}
	_ = start
	_ = scrollbackCount
	out := &protocolpb.Snapshot{
		Cols:          uint32(cols),
		Rows:          uint32(viewRows),
		Runes:         runes,
		Modes:         modes,
		Fg:            fg,
		Bg:            bg,
		Graphemes:     graphemes,
		Mode:          live.Mode,
		Title:         live.Title,
		CursorVisible: false,
	}
	if live.Cursor != nil && viewRows > 0 && cols > 0 {
		// Keep a visible cursor anchor inside the viewport while in scrollback mode.
		// This preserves copy-mode ergonomics in terminals like vterm.
		out.Cursor = &protocolpb.Cursor{X: 0, Y: 0}
		out.CursorVisible = true
	}
	return out
}

// ScrollbackPercent reports the visible position in the scrollback buffer.
func ScrollbackPercent(totalRows, viewRows, offset int) int {
	maxOffset := totalRows - viewRows
	if maxOffset <= 0 {
		return 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	percent := 100 - (offset*100)/maxOffset
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
