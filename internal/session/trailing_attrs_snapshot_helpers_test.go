package session_test

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/terminal"
)

func snapshotCellAt(snap terminal.Snapshot, row, col int) (terminal.Cell, bool) {
	if row < 1 || col < 1 || row > snap.Rows || col > snap.Cols {
		return terminal.Cell{}, false
	}
	idx := (row-1)*snap.Cols + (col - 1)
	if idx < 0 || idx >= len(snap.Cells) {
		return terminal.Cell{}, false
	}
	return snap.Cells[idx], true
}

func snapshotRows(snap terminal.Snapshot) []string {
	lines := make([]string, snap.Rows)
	for y := 0; y < snap.Rows; y++ {
		var row strings.Builder
		for x := 0; x < snap.Cols; x++ {
			idx := y*snap.Cols + x
			if idx < 0 || idx >= len(snap.Cells) {
				row.WriteRune(' ')
				continue
			}
			cell := snap.Cells[idx]
			if cell.Mode&terminal.ModeHidden != 0 {
				row.WriteRune(' ')
				continue
			}
			if cell.Grapheme != "" {
				row.WriteString(cell.Grapheme)
				if w := runewidth.StringWidth(cell.Grapheme); w > 1 {
					x += w - 1
				}
				continue
			}
			if cell.Rune == 0 {
				row.WriteRune(' ')
			} else {
				row.WriteRune(cell.Rune)
			}
		}
		lines[y] = row.String()
	}
	return lines
}
