package host

import "pkt.systems/lingon/internal/protocolpb"

func diffSnapshots(prev, next *protocolpb.Snapshot) (*protocolpb.Diff, bool) {
	if next == nil {
		return nil, false
	}
	if prev == nil || prev.Cols != next.Cols || prev.Rows != next.Rows {
		return nil, true
	}

	diff := &protocolpb.Diff{
		Cols:          next.Cols,
		Rows:          next.Rows,
		Cursor:        next.Cursor,
		CursorVisible: next.CursorVisible,
		Mode:          next.Mode,
		Title:         next.Title,
	}

	cols := int(next.Cols)
	rows := int(next.Rows)
	changed := false

	for y := 0; y < rows; y++ {
		if !rowChanged(prev, next, y, cols) {
			continue
		}
		row := &protocolpb.DiffRow{
			Row:   uint32(y),
			Runes: nextRow(next.Runes, y, cols),
			Modes: nextRowInt32(next.Modes, y, cols),
			Fg:    nextRow(next.Fg, y, cols),
			Bg:    nextRow(next.Bg, y, cols),
		}
		diff.DiffRows = append(diff.DiffRows, row)
		changed = true
	}

	metaChanged := !cursorEqual(prev.Cursor, next.Cursor) ||
		prev.CursorVisible != next.CursorVisible ||
		prev.Mode != next.Mode ||
		prev.Title != next.Title

	if !changed && !metaChanged {
		return nil, false
	}
	return diff, false
}

func rowChanged(prev, next *protocolpb.Snapshot, row, cols int) bool {
	for x := 0; x < cols; x++ {
		idx := row*cols + x
		if idx >= len(prev.Runes) || idx >= len(next.Runes) {
			return true
		}
		if prev.Runes[idx] != next.Runes[idx] {
			return true
		}
		if idx < len(prev.Modes) && idx < len(next.Modes) {
			if prev.Modes[idx] != next.Modes[idx] {
				return true
			}
		} else if len(prev.Modes) != len(next.Modes) {
			return true
		}
		if idx < len(prev.Fg) && idx < len(next.Fg) {
			if prev.Fg[idx] != next.Fg[idx] {
				return true
			}
		} else if len(prev.Fg) != len(next.Fg) {
			return true
		}
		if idx < len(prev.Bg) && idx < len(next.Bg) {
			if prev.Bg[idx] != next.Bg[idx] {
				return true
			}
		} else if len(prev.Bg) != len(next.Bg) {
			return true
		}
	}
	return false
}

func nextRow(data []uint32, row, cols int) []uint32 {
	out := make([]uint32, cols)
	start := row * cols
	for i := 0; i < cols; i++ {
		idx := start + i
		if idx < len(data) {
			out[i] = data[idx]
		}
	}
	return out
}

func nextRowInt32(data []int32, row, cols int) []int32 {
	out := make([]int32, cols)
	start := row * cols
	for i := 0; i < cols; i++ {
		idx := start + i
		if idx < len(data) {
			out[i] = data[idx]
		}
	}
	return out
}

func cursorEqual(a, b *protocolpb.Cursor) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.X == b.X && a.Y == b.Y
}
