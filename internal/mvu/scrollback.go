package mvu

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// BuildScrollbackViewFromTerminal composes a snapshot view that includes
// terminal scrollback rows followed by live rows at the current offset.
func BuildScrollbackViewFromTerminal(cols, viewRows int, scrollback []terminal.ScrollbackRow, live *protocolpb.Snapshot, offset, colOffset int) *protocolpb.Snapshot {
	if live == nil {
		return &protocolpb.Snapshot{Cols: uint32(cols), Rows: uint32(viewRows)}
	}
	contentCols := scrollbackTerminalWidth(scrollback, live)
	cols, viewRows, start, startCol, _ := normalizeScrollbackBounds(cols, viewRows, contentCols, len(scrollback), live, offset, colOffset)
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
		for x := 0; x < cols; x++ {
			srcCol := startCol + x
			if srcCol < 0 || srcCol >= len(cells) {
				continue
			}
			idx := startIdx + x
			cell := cells[srcCol]
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
					srcCol := startCol + x
					if srcCol < 0 || srcCol >= int(live.Cols) {
						continue
					}
					idx := rowStart + srcCol
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
func BuildScrollbackViewFromProto(cols, viewRows int, scrollback []*protocolpb.ScrollbackRow, live *protocolpb.Snapshot, offset, colOffset int) *protocolpb.Snapshot {
	if live == nil {
		return &protocolpb.Snapshot{Cols: uint32(cols), Rows: uint32(viewRows)}
	}
	contentCols := scrollbackProtoWidth(scrollback, live)
	cols, viewRows, start, startCol, _ := normalizeScrollbackBounds(cols, viewRows, contentCols, len(scrollback), live, offset, colOffset)
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
			srcCol := startCol + x
			idx := startIdx + x
			if srcCol >= 0 && srcCol < len(row.Runes) {
				runes[idx] = row.Runes[srcCol]
			}
			if srcCol >= 0 && srcCol < len(row.Modes) {
				modes[idx] = row.Modes[srcCol]
			}
			if srcCol >= 0 && srcCol < len(row.Fg) {
				fg[idx] = row.Fg[srcCol]
			}
			if srcCol >= 0 && srcCol < len(row.Bg) {
				bg[idx] = row.Bg[srcCol]
			}
			if graphemes != nil && srcCol >= 0 && srcCol < len(row.Graphemes) {
				graphemes[idx] = row.Graphemes[srcCol]
				if row.Graphemes[srcCol] != "" {
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
					srcCol := startCol + x
					if srcCol < 0 || srcCol >= int(live.Cols) {
						continue
					}
					idx := rowStart + srcCol
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

func normalizeScrollbackBounds(cols, viewRows, contentCols, scrollbackCount int, live *protocolpb.Snapshot, offset, colOffset int) (int, int, int, int, int) {
	if cols <= 0 {
		cols = contentCols
	}
	if viewRows <= 0 {
		viewRows = int(live.Rows)
	}
	if contentCols <= 0 {
		contentCols = int(live.Cols)
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
	maxCol := contentCols - cols
	if maxCol < 0 {
		maxCol = 0
	}
	if colOffset < 0 {
		colOffset = 0
	}
	if colOffset > maxCol {
		colOffset = maxCol
	}
	return cols, viewRows, start, colOffset, offset
}

func scrollbackTerminalWidth(scrollback []terminal.ScrollbackRow, live *protocolpb.Snapshot) int {
	width := 0
	if live != nil {
		width = int(live.Cols)
	}
	for _, row := range scrollback {
		if len(row.Cells) > width {
			width = len(row.Cells)
		}
	}
	return width
}

func scrollbackProtoWidth(scrollback []*protocolpb.ScrollbackRow, live *protocolpb.Snapshot) int {
	width := 0
	if live != nil {
		width = int(live.Cols)
	}
	for _, row := range scrollback {
		if row == nil {
			continue
		}
		for _, candidate := range []int{len(row.Runes), len(row.Modes), len(row.Fg), len(row.Bg), len(row.Graphemes)} {
			if candidate > width {
				width = candidate
			}
		}
	}
	return width
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
