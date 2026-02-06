package mvu

import "pkt.systems/lingon/internal/protocolpb"

// ScrollbackViewport tracks MVU scrollback view state independently of storage.
type ScrollbackViewport struct {
	active bool
	offset int
}

// Enter enables scrollback view and resets offset.
func (v *ScrollbackViewport) Enter() {
	if v == nil {
		return
	}
	v.active = true
	v.offset = 0
}

// Exit disables scrollback view and resets offset.
func (v *ScrollbackViewport) Exit() {
	if v == nil {
		return
	}
	v.active = false
	v.offset = 0
}

// SetActive enables or disables scrollback view.
func (v *ScrollbackViewport) SetActive(active bool) {
	if v == nil {
		return
	}
	if active {
		v.active = true
		return
	}
	v.active = false
	v.offset = 0
}

// Active reports whether scrollback view mode is enabled.
func (v *ScrollbackViewport) Active() bool {
	if v == nil {
		return false
	}
	return v.active
}

// Offset reports the current scrollback offset.
func (v *ScrollbackViewport) Offset() int {
	if v == nil {
		return 0
	}
	return v.offset
}

// Visible reports whether scrollback overlay should be visible.
func (v *ScrollbackViewport) Visible() bool {
	if v == nil {
		return false
	}
	return v.active || v.offset > 0
}

// Normalize clamps current offset to available range.
func (v *ScrollbackViewport) Normalize(totalRows, viewRows int) {
	if v == nil {
		return
	}
	max := scrollbackMaxOffset(totalRows, viewRows)
	if v.offset < 0 {
		v.offset = 0
	}
	if v.offset > max {
		v.offset = max
	}
}

// SetOffset sets an explicit offset clamped to available range.
func (v *ScrollbackViewport) SetOffset(totalRows, viewRows, offset int) bool {
	if v == nil {
		return false
	}
	max := scrollbackMaxOffset(totalRows, viewRows)
	prev := v.offset
	if offset < 0 {
		offset = 0
	}
	if offset > max {
		offset = max
	}
	v.offset = offset
	return prev != v.offset
}

// Page adjusts offset by stepRows*delta and reports whether state changed.
func (v *ScrollbackViewport) Page(totalRows, viewRows, delta, stepRows int) bool {
	if v == nil {
		return false
	}
	if viewRows <= 0 {
		return false
	}
	if stepRows <= 0 {
		stepRows = viewRows
	}
	max := scrollbackMaxOffset(totalRows, viewRows)
	prev := v.offset
	next := v.offset + delta*stepRows
	if next < 0 {
		next = 0
	}
	if next > max {
		next = max
	}
	v.offset = next
	if next == prev && delta > 0 && next == max {
		return false
	}
	return next != prev
}

// Top jumps to the oldest available visible segment.
func (v *ScrollbackViewport) Top(totalRows, viewRows int) bool {
	if v == nil {
		return false
	}
	max := scrollbackMaxOffset(totalRows, viewRows)
	if v.offset == max {
		return false
	}
	v.offset = max
	return true
}

// Bottom jumps back to live view.
func (v *ScrollbackViewport) Bottom() bool {
	if v == nil {
		return false
	}
	if v.offset == 0 {
		return false
	}
	v.offset = 0
	return true
}

// Percent reports current scrollback percent.
func (v *ScrollbackViewport) Percent(totalRows, viewRows int) int {
	if v == nil {
		return 100
	}
	return ScrollbackPercent(totalRows, viewRows, v.offset)
}

func scrollbackMaxOffset(totalRows, viewRows int) int {
	maxOffset := totalRows - viewRows
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

// ProtoScrollbackBuffer owns attach-side protocol scrollback rows.
type ProtoScrollbackBuffer struct {
	rows  []*protocolpb.ScrollbackRow
	cols  int
	limit int
}

// NewProtoScrollbackBuffer constructs a protocol scrollback buffer.
func NewProtoScrollbackBuffer(limit int) *ProtoScrollbackBuffer {
	return &ProtoScrollbackBuffer{limit: limit}
}

// SetLimit updates row limit and trims existing rows.
func (b *ProtoScrollbackBuffer) SetLimit(limit int) {
	if b == nil {
		return
	}
	b.limit = limit
	b.trim()
}

// Apply ingests a protocol scrollback update.
func (b *ProtoScrollbackBuffer) Apply(scrollback *protocolpb.Scrollback) {
	if b == nil || scrollback == nil {
		return
	}
	if scrollback.Clear {
		b.rows = nil
	}
	if scrollback.Cols > 0 {
		cols := int(scrollback.Cols)
		if b.cols != 0 && b.cols != cols {
			b.rows = nil
		}
		b.cols = cols
	}
	for _, row := range scrollback.Rows {
		b.rows = append(b.rows, cloneProtoScrollbackRow(row))
	}
	b.trim()
}

func (b *ProtoScrollbackBuffer) trim() {
	if b == nil || b.limit <= 0 || len(b.rows) <= b.limit {
		return
	}
	extra := len(b.rows) - b.limit
	b.rows = b.rows[extra:]
}

// Len reports stored row count.
func (b *ProtoScrollbackBuffer) Len() int {
	if b == nil {
		return 0
	}
	return len(b.rows)
}

// Rows returns a deep-cloned snapshot of stored rows.
func (b *ProtoScrollbackBuffer) Rows() []*protocolpb.ScrollbackRow {
	if b == nil || len(b.rows) == 0 {
		return nil
	}
	out := make([]*protocolpb.ScrollbackRow, len(b.rows))
	for i := range b.rows {
		out[i] = cloneProtoScrollbackRow(b.rows[i])
	}
	return out
}

func cloneProtoScrollbackRow(row *protocolpb.ScrollbackRow) *protocolpb.ScrollbackRow {
	if row == nil {
		return &protocolpb.ScrollbackRow{}
	}
	out := &protocolpb.ScrollbackRow{}
	if len(row.Runes) > 0 {
		out.Runes = append(out.Runes, row.Runes...)
	}
	if len(row.Modes) > 0 {
		out.Modes = append(out.Modes, row.Modes...)
	}
	if len(row.Fg) > 0 {
		out.Fg = append(out.Fg, row.Fg...)
	}
	if len(row.Bg) > 0 {
		out.Bg = append(out.Bg, row.Bg...)
	}
	if len(row.Graphemes) > 0 {
		out.Graphemes = append(out.Graphemes, row.Graphemes...)
	}
	return out
}
