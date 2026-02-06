package mvu

import (
	"bytes"
	"fmt"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

// ComposeViewportSnapshot builds the full composed viewport framebuffer snapshot.
// DO NOT REMOVE: hard requirement.
// All overlays must be composed into one framebuffer model before terminal diff.
func ComposeViewportSnapshot(base *protocolpb.Snapshot, cols, rows int, resolved Resolved, cursor Cursor) (*protocolpb.Snapshot, error) {
	if cols <= 0 || rows <= 0 {
		return BlankSnapshot(1, 1), nil
	}
	if base == nil {
		base = BlankSnapshot(cols, rows)
	}
	var baseBuf bytes.Buffer
	if err := render.SnapshotViewportNoClear(&baseBuf, base, cols, rows); err != nil {
		return nil, err
	}
	overlay := ComposeResolved(nil, cols, rows, cursor, resolved)
	return composeLayersToSnapshot(cols, rows, baseBuf.Bytes(), overlay)
}

// ComposeDisabledViewportSnapshot builds the dimmed composed viewport framebuffer snapshot.
func ComposeDisabledViewportSnapshot(base *protocolpb.Snapshot, cols, rows int, resolved Resolved, cursor Cursor) (*protocolpb.Snapshot, error) {
	if cols <= 0 || rows <= 0 {
		return BlankSnapshot(1, 1), nil
	}
	if base == nil {
		base = BlankSnapshot(cols, rows)
	}
	var baseBuf bytes.Buffer
	if err := RenderSnapshotViewportDim(&baseBuf, base, cols, rows); err != nil {
		return nil, err
	}
	overlay := ComposeResolved(nil, cols, rows, cursor, resolved)
	return composeLayersToSnapshot(cols, rows, baseBuf.Bytes(), overlay)
}

func composeLayersToSnapshot(cols, rows int, layers ...[]byte) (*protocolpb.Snapshot, error) {
	e := emu.New(cols, rows)
	for _, layer := range layers {
		if len(layer) == 0 {
			continue
		}
		if err := e.Write(layer); err != nil {
			return nil, err
		}
	}
	snap, err := e.Snapshot()
	if err != nil {
		return nil, err
	}
	return protoSnapshotFromTerminalSnapshot(snap)
}

func protoSnapshotFromTerminalSnapshot(s terminal.Snapshot) (*protocolpb.Snapshot, error) {
	if s.Cols <= 0 || s.Rows <= 0 {
		return BlankSnapshot(1, 1), nil
	}
	size := s.Cols * s.Rows
	if len(s.Cells) < size {
		return nil, fmt.Errorf("terminal snapshot cells mismatch: have=%d need=%d", len(s.Cells), size)
	}
	out := &protocolpb.Snapshot{
		Cols:          uint32(s.Cols),
		Rows:          uint32(s.Rows),
		Runes:         make([]uint32, size),
		Modes:         make([]int32, size),
		Fg:            make([]uint32, size),
		Bg:            make([]uint32, size),
		Cursor:        &protocolpb.Cursor{X: uint32(s.Cursor.X), Y: uint32(s.Cursor.Y)},
		CursorVisible: s.CursorVisible,
		Mode:          s.Mode,
		Title:         s.Title,
	}
	hasGrapheme := false
	for i := 0; i < size; i++ {
		cell := s.Cells[i]
		out.Runes[i] = uint32(cell.Rune)
		out.Modes[i] = int32(cell.Mode)
		out.Fg[i] = cell.FG
		out.Bg[i] = cell.BG
		if cell.Grapheme != "" {
			hasGrapheme = true
		}
	}
	if hasGrapheme {
		out.Graphemes = make([]string, size)
		for i := 0; i < size; i++ {
			out.Graphemes[i] = s.Cells[i].Grapheme
		}
	}
	return out, nil
}
