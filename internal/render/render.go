package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

const (
	ansiClearScreen = "\x1b[2J"
	ansiHome        = "\x1b[H"
	ansiHideCursor  = "\x1b[?25l"
	ansiShowCursor  = "\x1b[?25h"
	ansiReset       = "\x1b[0m"
	ansiClearLine   = "\x1b[K"
)

// Snapshot renders a snapshot to the writer using ANSI escapes.
func Snapshot(w io.Writer, snap *protocolpb.Snapshot) error {
	if snap == nil {
		return nil
	}
	return SnapshotViewport(w, snap, int(snap.Cols), int(snap.Rows))
}

// SnapshotViewportDelta renders only changed rows when possible to reduce flicker.
// It falls back to full SnapshotViewport when sizes or viewport origin change.
func SnapshotViewportDelta(w io.Writer, prev, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if prev == nil || prev.Cols != snap.Cols || prev.Rows != snap.Rows {
		return SnapshotViewport(w, snap, viewCols, viewRows)
	}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	prevCursorX := int(prev.Cursor.GetX())
	prevCursorY := int(prev.Cursor.GetY())
	if prevCursorX < 0 {
		prevCursorX = 0
	}
	if prevCursorY < 0 {
		prevCursorY = 0
	}
	if prevCursorX >= cols {
		prevCursorX = cols - 1
	}
	if prevCursorY >= rows {
		prevCursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	px0, py0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, prevCursorX, prevCursorY)
	if x0 != px0 || y0 != py0 {
		return SnapshotViewportNoClear(w, snap, viewCols, viewRows)
	}

	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != prev.Title {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}

	for y := 0; y < viewRows; y++ {
		cy := y0 + y
		runs := changedRuns(prev, snap, cy, x0, viewCols, cols, rows)
		for _, run := range runs {
			start := run[0]
			end := run[1]
			if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", y+1, start+1)); err != nil {
				return err
			}
			if err := writeDeltaRun(snap, cy, x0, start, end, viewCols, cols, rows, defaultAttr, w); err != nil {
				return err
			}
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0
	if viewX < 0 {
		viewX = 0
	}
	if viewY < 0 {
		viewY = 0
	}
	if viewX >= viewCols {
		viewX = viewCols - 1
	}
	if viewY >= viewRows {
		viewY = viewRows - 1
	}
	if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+1, viewX+1)); err != nil {
		return err
	}

	return nil
}

// SnapshotViewportDeltaSkipTopRow renders changed rows but skips the first row.
func SnapshotViewportDeltaSkipTopRow(w io.Writer, prev, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if prev == nil || prev.Cols != snap.Cols || prev.Rows != snap.Rows {
		return SnapshotViewportSkipTopRow(w, snap, viewCols, viewRows)
	}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	prevCursorX := int(prev.Cursor.GetX())
	prevCursorY := int(prev.Cursor.GetY())
	if prevCursorX < 0 {
		prevCursorX = 0
	}
	if prevCursorY < 0 {
		prevCursorY = 0
	}
	if prevCursorX >= cols {
		prevCursorX = cols - 1
	}
	if prevCursorY >= rows {
		prevCursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	px0, py0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, prevCursorX, prevCursorY)
	if x0 != px0 || y0 != py0 {
		return SnapshotViewportNoClearSkipTopRow(w, snap, viewCols, viewRows)
	}

	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != prev.Title {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	contentRows := viewRows - 1
	if contentRows < 0 {
		contentRows = 0
	}
	for sy := 0; sy < contentRows; sy++ {
		cy := y0 + sy
		ty := sy + 2
		runs := changedRuns(prev, snap, cy, x0, viewCols, cols, rows)
		for _, run := range runs {
			start := run[0]
			end := run[1]
			if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", ty, start+1)); err != nil {
				return err
			}
			if err := writeDeltaRun(snap, cy, x0, start, end, viewCols, cols, rows, defaultAttr, w); err != nil {
				return err
			}
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0
	if viewX >= 0 && viewX < viewCols && viewY >= 0 && viewY < contentRows {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+2, viewX+1)); err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotViewport renders a snapshot cropped or padded to a viewport.
func SnapshotViewport(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if _, err := io.WriteString(w, ansiClearScreen+ansiHome); err != nil {
		return err
	}
	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	for y := 0; y < viewRows; y++ {
		cy := y0 + y
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", y+1, 1)); err != nil {
			return err
		}
		row := buildRow(snap, cy, x0, viewCols, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, row); err != nil {
			return err
		}
	}

	// Move cursor to position (1-based).
	cursorRow := uint32(0)
	cursorCol := uint32(0)
	if cursorX >= x0 && cursorX < x0+viewCols && cursorY >= y0 && cursorY < y0+viewRows {
		cursorRow = uint32(cursorY-y0) + 1
		cursorCol = uint32(cursorX-x0) + 1
	}
	if cursorRow > 0 && cursorCol > 0 {
		_, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", cursorRow, cursorCol))
		if err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != "" {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	return nil
}

// ViewportRow renders a single viewport row (1-based) from the snapshot.
func ViewportRow(snap *protocolpb.Snapshot, viewRow, viewCols, viewRows int) (string, bool) {
	if snap == nil || viewRow < 1 {
		return "", false
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if cols <= 0 || rows <= 0 {
		return "", false
	}
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}
	if viewRow > viewRows {
		return "", false
	}
	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}
	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	row := y0 + (viewRow - 1)
	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	return buildRow(snap, row, x0, viewCols, cols, rows, defaultAttr), true
}

// SnapshotViewportSkipTopRow renders a snapshot while skipping the first row.
func SnapshotViewportSkipTopRow(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if _, err := io.WriteString(w, ansiClearScreen+ansiHome); err != nil {
		return err
	}
	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	contentRows := viewRows - 1
	if contentRows < 0 {
		contentRows = 0
	}
	for sy := 0; sy < contentRows; sy++ {
		cy := y0 + sy
		ty := sy + 2
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", ty, 1)); err != nil {
			return err
		}
		row := buildRow(snap, cy, x0, viewCols, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, row); err != nil {
			return err
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0
	if viewX >= 0 && viewX < viewCols && viewY >= 0 && viewY < contentRows {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+2, viewX+1)); err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != "" {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotViewportNoClear renders a snapshot without clearing the whole screen.
func SnapshotViewportNoClear(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	for y := 0; y < viewRows; y++ {
		cy := y0 + y
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", y+1, 1)); err != nil {
			return err
		}
		row := buildRow(snap, cy, x0, viewCols, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, row); err != nil {
			return err
		}
	}

	// Move cursor to position (1-based).
	cursorRow := uint32(0)
	cursorCol := uint32(0)
	if cursorX >= x0 && cursorX < x0+viewCols && cursorY >= y0 && cursorY < y0+viewRows {
		cursorRow = uint32(cursorY-y0) + 1
		cursorCol = uint32(cursorX-x0) + 1
	}
	if cursorRow > 0 && cursorCol > 0 {
		_, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", cursorRow, cursorCol))
		if err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != "" {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotViewportNoClearSkipTopRow renders a snapshot without clearing, skipping the first row.
func SnapshotViewportNoClearSkipTopRow(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	contentRows := viewRows - 1
	if contentRows < 0 {
		contentRows = 0
	}
	for sy := 0; sy < contentRows; sy++ {
		cy := y0 + sy
		ty := sy + 2
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", ty, 1)); err != nil {
			return err
		}
		row := buildRow(snap, cy, x0, viewCols, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, row); err != nil {
			return err
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0
	if viewX >= 0 && viewX < viewCols && viewY >= 0 && viewY < contentRows {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+2, viewX+1)); err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != "" {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotViewportNoClearMaskTopRow renders a snapshot without clearing while
// preserving row mapping and skipping terminal row 1 writes.
func SnapshotViewportNoClearMaskTopRow(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	contentRows := viewRows - 1
	if contentRows < 0 {
		contentRows = 0
	}
	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	for sy := 0; sy < contentRows; sy++ {
		cy := y0 + sy + 1
		ty := sy + 2
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", ty, 1)); err != nil {
			return err
		}
		row := buildRow(snap, cy, x0, viewCols, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, row); err != nil {
			return err
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0 - 1
	if viewX >= 0 && viewX < viewCols && viewY >= 0 && viewY < contentRows {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+2, viewX+1)); err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != "" {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotViewportDeltaMaskTopRow renders changed rows while preserving row
// mapping and skipping terminal row 1 writes.
func SnapshotViewportDeltaMaskTopRow(w io.Writer, prev, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if prev == nil || prev.Cols != snap.Cols || prev.Rows != snap.Rows {
		return SnapshotViewportNoClearMaskTopRow(w, snap, viewCols, viewRows)
	}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}

	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	prevCursorX := int(prev.Cursor.GetX())
	prevCursorY := int(prev.Cursor.GetY())
	if prevCursorX < 0 {
		prevCursorX = 0
	}
	if prevCursorY < 0 {
		prevCursorY = 0
	}
	if prevCursorX >= cols {
		prevCursorX = cols - 1
	}
	if prevCursorY >= rows {
		prevCursorY = rows - 1
	}

	contentRows := viewRows - 1
	if contentRows < 0 {
		contentRows = 0
	}
	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	px0, py0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, prevCursorX, prevCursorY)
	if x0 != px0 || y0 != py0 {
		return SnapshotViewportNoClearMaskTopRow(w, snap, viewCols, viewRows)
	}

	if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiShowCursor); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	if snap.Title != prev.Title {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b]0;%s\x07", sanitizeTitle(snap.Title))); err != nil {
			return err
		}
	}

	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	for sy := 0; sy < contentRows; sy++ {
		cy := y0 + sy + 1
		ty := sy + 2
		runs := changedRuns(prev, snap, cy, x0, viewCols, cols, rows)
		for _, run := range runs {
			start := run[0]
			end := run[1]
			if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", ty, start+1)); err != nil {
				return err
			}
			if err := writeDeltaRun(snap, cy, x0, start, end, viewCols, cols, rows, defaultAttr, w); err != nil {
				return err
			}
		}
	}

	viewX := cursorX - x0
	viewY := cursorY - y0 - 1
	if viewX >= 0 && viewX < viewCols && viewY >= 0 && viewY < contentRows {
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", viewY+2, viewX+1)); err != nil {
			return err
		}
	} else if snap.CursorVisible {
		if _, err := io.WriteString(w, ansiHideCursor); err != nil {
			return err
		}
	}

	return nil
}

// ViewportRowSpan returns one visible viewport row segment for the current
// cursor-driven viewport origin. start and end are zero-based columns within the
// visible viewport, with end exclusive.
func ViewportRowSpan(snap *protocolpb.Snapshot, viewCols, viewRows, row, start, end int) string {
	if snap == nil {
		return ""
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}
	if row < 0 || row >= viewRows {
		return ""
	}
	if start < 0 {
		start = 0
	}
	if end > viewCols {
		end = viewCols
	}
	if end <= start {
		return ""
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cols > 0 && cursorX >= cols {
		cursorX = cols - 1
	}
	if rows > 0 && cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	return buildRowSpan(snap, y0+row, x0, start, end, cols, rows, defaultAttr)
}

// SnapshotViewportDim renders a snapshot in dimmed grayscale for disabled views.
func SnapshotViewportDim(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	if snap == nil {
		return nil
	}
	if _, err := io.WriteString(w, ansiHideCursor); err != nil {
		return err
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}

	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}

	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	dimOn := "\x1b[2m\x1b[90m"
	dimOff := ansiReset

	for y := 0; y < viewRows; y++ {
		cy := y0 + y
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", y+1, 1)); err != nil {
			return err
		}
		last := -1
		for x := 0; x < viewCols; x++ {
			cx := x0 + x
			r := ' '
			g := ""
			span := 0
			if cx >= 0 && cy >= 0 && cx < cols && cy < rows {
				idx := cy*cols + cx
				g = graphemeAt(snap, idx)
				if g == "" && idx < len(snap.Runes) {
					r = rune(snap.Runes[idx])
				}
				var attr renderAttr
				if idx < len(snap.Modes) {
					attr.mode = snap.Modes[idx]
				}
				if idx < len(snap.Fg) {
					attr.fg = snap.Fg[idx]
				}
				if idx < len(snap.Bg) {
					attr.bg = snap.Bg[idx]
				}
				if x+1 < viewCols && isContinuationCell(snap, idx+1, attr) {
					span = 1
				}
				if attr.mode&int32(terminal.ModeHidden) != 0 {
					g = ""
					r = ' '
				}
			}
			if g != "" || (r != 0 && r != ' ') {
				if x > last {
					last = x
				}
				if span > 0 && x+span > last {
					last = x + span
				}
			}
		}
		var rowBuilder strings.Builder
		rowBuilder.WriteString(dimOn)
		if last < 0 {
			rowBuilder.WriteString(ansiClearLine)
		} else {
			for x := 0; x < viewCols && x <= last; x++ {
				cx := x0 + x
				r := ' '
				g := ""
				skipNext := false
				if cx >= 0 && cy >= 0 && cx < cols && cy < rows {
					idx := cy*cols + cx
					g = graphemeAt(snap, idx)
					if g == "" && idx < len(snap.Runes) {
						r = rune(snap.Runes[idx])
					}
					var attr renderAttr
					if idx < len(snap.Modes) {
						attr.mode = snap.Modes[idx]
					}
					if idx < len(snap.Fg) {
						attr.fg = snap.Fg[idx]
					}
					if idx < len(snap.Bg) {
						attr.bg = snap.Bg[idx]
					}
					if x+1 < viewCols && isContinuationCell(snap, idx+1, attr) {
						skipNext = true
					}
					if attr.mode&int32(terminal.ModeHidden) != 0 {
						g = ""
						r = ' '
					}
				}
				if g != "" {
					rowBuilder.WriteString(g)
				} else {
					if r == 0 {
						r = ' '
					}
					rowBuilder.WriteRune(r)
				}
				if skipNext {
					x++
				}
			}
			if last < viewCols-1 {
				rowBuilder.WriteString(ansiClearLine)
			}
		}
		if _, err := io.WriteString(w, rowBuilder.String()); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, dimOff)
	return err
}

type renderAttr struct {
	mode int32
	fg   uint32
	bg   uint32
}

func attrEqual(a, b renderAttr) bool {
	return a.mode == b.mode && a.fg == b.fg && a.bg == b.bg
}

func cellEqual(prev, snap *protocolpb.Snapshot, row, col, cols, rows int) bool {
	if row < 0 || row >= rows || col < 0 || col >= cols {
		return true
	}
	idx := row*cols + col
	prevG := graphemeAt(prev, idx)
	nextG := graphemeAt(snap, idx)
	if prevG != "" || nextG != "" {
		if prevG != nextG {
			return false
		}
	}
	if idx >= len(prev.Runes) || idx >= len(snap.Runes) {
		return false
	}
	if prev.Runes[idx] != snap.Runes[idx] {
		return false
	}
	if idx < len(prev.Modes) && idx < len(snap.Modes) {
		if prev.Modes[idx] != snap.Modes[idx] {
			return false
		}
	} else if len(prev.Modes) != len(snap.Modes) {
		return false
	}
	if idx < len(prev.Fg) && idx < len(snap.Fg) {
		if prev.Fg[idx] != snap.Fg[idx] {
			return false
		}
	} else if len(prev.Fg) != len(snap.Fg) {
		return false
	}
	if idx < len(prev.Bg) && idx < len(snap.Bg) {
		if prev.Bg[idx] != snap.Bg[idx] {
			return false
		}
	} else if len(prev.Bg) != len(snap.Bg) {
		return false
	}
	return true
}

func changedRuns(prev, snap *protocolpb.Snapshot, row, x0, viewCols, cols, rows int) [][2]int {
	if row < 0 || row >= rows {
		return nil
	}
	runs := make([][2]int, 0, 4)
	start := -1
	for x := 0; x < viewCols; x++ {
		cx := x0 + x
		changed := !cellEqual(prev, snap, row, cx, cols, rows)
		if changed {
			if start < 0 {
				start = x
			}
			continue
		}
		if start >= 0 {
			runs = append(runs, [2]int{start, x - 1})
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, [2]int{start, viewCols - 1})
	}
	return runs
}

func writeDeltaRun(snap *protocolpb.Snapshot, row, x0, start, end, viewCols, cols, rows int, defaultAttr renderAttr, w io.Writer) error {
	last := lastNonDefaultCol(snap, row, x0, viewCols, cols, rows, defaultAttr)
	drawStart := start
	drawEnd := end
	clearTail := false
	if drawEnd > last {
		drawEnd = last
		clearTail = true
	}
	if drawEnd >= drawStart {
		if _, err := io.WriteString(w, ansiReset); err != nil {
			return err
		}
		span := buildRowSpan(snap, row, x0, drawStart, drawEnd, cols, rows, defaultAttr)
		if _, err := io.WriteString(w, span); err != nil {
			return err
		}
	}
	// Prefer EL for trailing default region so copy/select semantics match
	// terminals that trim line tails rather than preserving literal spaces.
	if clearTail && last < viewCols-1 {
		if _, err := io.WriteString(w, ansiReset+ansiClearLine); err != nil {
			return err
		}
	}
	return nil
}

func isDefaultCell(attr renderAttr, g string, r rune, defaultAttr renderAttr) bool {
	if g != "" {
		return false
	}
	if r != 0 && r != ' ' {
		return false
	}
	return attrEqual(attr, defaultAttr)
}

func lastNonDefaultCol(snap *protocolpb.Snapshot, row, x0, viewCols, cols, rows int, defaultAttr renderAttr) int {
	if row < 0 || row >= rows {
		return -1
	}
	last := -1
	for x := 0; x < viewCols; x++ {
		cx := x0 + x
		attr := defaultAttr
		r := ' '
		g := ""
		span := 0
		if cx >= 0 && cx < cols {
			idx := row*cols + cx
			g = graphemeAt(snap, idx)
			if g == "" && idx < len(snap.Runes) {
				r = rune(snap.Runes[idx])
			}
			if idx < len(snap.Modes) {
				attr.mode = snap.Modes[idx]
			}
			if idx < len(snap.Fg) {
				attr.fg = snap.Fg[idx]
			}
			if idx < len(snap.Bg) {
				attr.bg = snap.Bg[idx]
			}
			wide := false
			if g != "" {
				wide = runewidth.StringWidth(g) > 1
			} else if r != 0 {
				wide = runewidth.RuneWidth(r) > 1
			}
			if wide && x+1 < viewCols && isContinuationCell(snap, idx+1, attr) {
				span = 1
			}
		}
		if attr.mode&int32(terminal.ModeHidden) != 0 {
			g = ""
			r = ' '
		}
		if !isDefaultCell(attr, g, r, defaultAttr) {
			if x > last {
				last = x
			}
			if span > 0 && x+span > last {
				last = x + span
			}
		}
	}
	return last
}

func buildRow(snap *protocolpb.Snapshot, row, x0, viewCols, cols, rows int, defaultAttr renderAttr) string {
	last := lastNonDefaultCol(snap, row, x0, viewCols, cols, rows, defaultAttr)
	var rowBuilder strings.Builder
	rowBuilder.WriteString(sgr(defaultAttr))
	current := defaultAttr
	if last < 0 {
		rowBuilder.WriteString(ansiClearLine)
		return rowBuilder.String()
	}
	for x := 0; x < viewCols && x <= last; x++ {
		cx := x0 + x
		attr := defaultAttr
		r := ' '
		g := ""
		skipNext := false
		if cx >= 0 && row >= 0 && cx < cols && row < rows {
			idx := row*cols + cx
			g = graphemeAt(snap, idx)
			if g == "" && idx < len(snap.Runes) {
				r = rune(snap.Runes[idx])
			}
			if idx < len(snap.Modes) {
				attr.mode = snap.Modes[idx]
			}
			if idx < len(snap.Fg) {
				attr.fg = snap.Fg[idx]
			}
			if idx < len(snap.Bg) {
				attr.bg = snap.Bg[idx]
			}
			wide := false
			if g != "" {
				wide = runewidth.StringWidth(g) > 1
			} else if r != 0 {
				wide = runewidth.RuneWidth(r) > 1
			}
			if wide && x+1 < viewCols && isContinuationCell(snap, idx+1, attr) {
				skipNext = true
			}
		}
		if attr.mode&int32(terminal.ModeHidden) != 0 {
			g = ""
			r = ' '
		}
		if !attrEqual(current, attr) {
			rowBuilder.WriteString(sgr(attr))
			current = attr
		}
		if g != "" {
			rowBuilder.WriteString(g)
		} else {
			if r == 0 {
				r = ' '
			}
			rowBuilder.WriteRune(r)
		}
		if skipNext {
			x++
		}
	}
	if last < viewCols-1 {
		if !attrEqual(current, defaultAttr) {
			rowBuilder.WriteString(sgr(defaultAttr))
		}
		rowBuilder.WriteString(ansiClearLine)
	} else if !attrEqual(current, defaultAttr) {
		rowBuilder.WriteString(ansiReset)
	}
	return rowBuilder.String()
}

func buildRowSpan(snap *protocolpb.Snapshot, row, x0, start, end, cols, rows int, defaultAttr renderAttr) string {
	if start > end {
		return ""
	}
	var rowBuilder strings.Builder
	current := defaultAttr
	for x := start; x <= end; x++ {
		cx := x0 + x
		attr := defaultAttr
		r := ' '
		g := ""
		skipNext := false
		if cx >= 0 && row >= 0 && cx < cols && row < rows {
			idx := row*cols + cx
			g = graphemeAt(snap, idx)
			if g == "" && idx < len(snap.Runes) {
				r = rune(snap.Runes[idx])
			}
			if idx < len(snap.Modes) {
				attr.mode = snap.Modes[idx]
			}
			if idx < len(snap.Fg) {
				attr.fg = snap.Fg[idx]
			}
			if idx < len(snap.Bg) {
				attr.bg = snap.Bg[idx]
			}
			wide := false
			if g != "" {
				wide = runewidth.StringWidth(g) > 1
			} else if r != 0 {
				wide = runewidth.RuneWidth(r) > 1
			}
			if wide && x+1 <= end && isContinuationCell(snap, idx+1, attr) {
				skipNext = true
			}
		}
		if attr.mode&int32(terminal.ModeHidden) != 0 {
			g = ""
			r = ' '
		}
		if !attrEqual(current, attr) {
			rowBuilder.WriteString(sgr(attr))
			current = attr
		}
		if g != "" {
			rowBuilder.WriteString(g)
		} else {
			if r == 0 {
				r = ' '
			}
			rowBuilder.WriteRune(r)
		}
		if skipNext {
			x++
		}
	}
	if !attrEqual(current, defaultAttr) {
		rowBuilder.WriteString(sgr(defaultAttr))
	}
	return rowBuilder.String()
}

func graphemeAt(snap *protocolpb.Snapshot, idx int) string {
	if snap == nil || idx < 0 {
		return ""
	}
	if idx < len(snap.Graphemes) {
		return snap.Graphemes[idx]
	}
	return ""
}

func isContinuationCell(snap *protocolpb.Snapshot, idx int, attr renderAttr) bool {
	if snap == nil || idx < 0 || idx >= len(snap.Runes) {
		return false
	}
	if graphemeAt(snap, idx) != "" {
		return false
	}
	if snap.Runes[idx] != 0 {
		return false
	}
	if idx < len(snap.Modes) && snap.Modes[idx] != attr.mode {
		return false
	}
	if idx < len(snap.Fg) && snap.Fg[idx] != attr.fg {
		return false
	}
	if idx < len(snap.Bg) && snap.Bg[idx] != attr.bg {
		return false
	}
	return true
}

func sgr(attr renderAttr) string {
	fg := attr.fg
	bg := attr.bg
	useInverse := attr.mode&int32(terminal.ModeInverse) != 0

	codes := []string{"0"}
	if attr.mode&int32(terminal.ModeBold) != 0 {
		codes = append(codes, "1")
	}
	if attr.mode&int32(terminal.ModeFaint) != 0 {
		codes = append(codes, "2")
	}
	if attr.mode&int32(terminal.ModeItalic) != 0 {
		codes = append(codes, "3")
	}
	if attr.mode&int32(terminal.ModeUnderline) != 0 {
		codes = append(codes, "4")
	}
	if attr.mode&int32(terminal.ModeBlink) != 0 {
		codes = append(codes, "5")
	}
	if useInverse {
		codes = append(codes, "7")
	}
	if attr.mode&int32(terminal.ModeHidden) != 0 {
		codes = append(codes, "8")
	}

	codes = append(codes, colorCode(true, fg)...)
	codes = append(codes, colorCode(false, bg)...)

	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func colorCode(fg bool, val uint32) []string {
	if val == terminal.ColorDefault {
		if fg {
			return []string{"39"}
		}
		return []string{"49"}
	}
	flag := val & terminal.ColorFlagMask
	raw := val & terminal.ColorValueMask
	if flag == terminal.ColorIndexed {
		if raw < 16 {
			if fg {
				if raw < 8 {
					return []string{strconv.FormatUint(uint64(30+raw), 10)}
				}
				return []string{strconv.FormatUint(uint64(90+(raw-8)), 10)}
			}
			if raw < 8 {
				return []string{strconv.FormatUint(uint64(40+raw), 10)}
			}
			return []string{strconv.FormatUint(uint64(100+(raw-8)), 10)}
		}
		if fg {
			return []string{"38", "5", strconv.FormatUint(uint64(raw), 10)}
		}
		return []string{"48", "5", strconv.FormatUint(uint64(raw), 10)}
	}
	if flag == terminal.ColorIndexed256 {
		if fg {
			return []string{"38", "5", strconv.FormatUint(uint64(raw), 10)}
		}
		return []string{"48", "5", strconv.FormatUint(uint64(raw), 10)}
	}
	if flag == terminal.ColorTrue {
		r := (raw >> 16) & 0xff
		g := (raw >> 8) & 0xff
		b := raw & 0xff
		if fg {
			return []string{"38", "2", strconv.FormatUint(uint64(r), 10), strconv.FormatUint(uint64(g), 10), strconv.FormatUint(uint64(b), 10)}
		}
		return []string{"48", "2", strconv.FormatUint(uint64(r), 10), strconv.FormatUint(uint64(g), 10), strconv.FormatUint(uint64(b), 10)}
	}
	if fg {
		return []string{"39"}
	}
	return []string{"49"}
}

func sanitizeTitle(title string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r':
			return -1
		default:
			return r
		}
	}, title)
}

// ViewportCursor returns the cursor position within the viewport (1-based) if visible.
func ViewportCursor(snap *protocolpb.Snapshot, viewCols, viewRows int) (row, col int, visible bool) {
	if snap == nil || !snap.CursorVisible || snap.Cursor == nil {
		return 0, 0, false
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}
	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}
	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	if cursorX < x0 || cursorX >= x0+viewCols || cursorY < y0 || cursorY >= y0+viewRows {
		return 0, 0, false
	}
	return cursorY - y0 + 1, cursorX - x0 + 1, true
}

// ViewportCursorPosition returns the cursor position within the viewport (1-based)
// regardless of cursor visibility. ok is false if the cursor is off-screen.
func ViewportCursorPosition(snap *protocolpb.Snapshot, viewCols, viewRows int) (row, col int, ok bool) {
	if snap == nil || snap.Cursor == nil {
		return 0, 0, false
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}
	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}
	x0, y0 := ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
	if cursorX < x0 || cursorX >= x0+viewCols || cursorY < y0 || cursorY >= y0+viewRows {
		return 0, 0, false
	}
	return cursorY - y0 + 1, cursorX - x0 + 1, true
}

// ViewportOriginForCursor resolves the viewport origin that keeps the cursor
// visible while preferring bottom-left alignment when the snapshot is larger
// than the viewport.
func ViewportOriginForCursor(cw, ch, vw, vh, cursorX, cursorY int) (int, int) {
	x0 := 0
	y0 := 0

	if vw < cw {
		if cursorX >= vw {
			x0 = cursorX - vw + 1
		}
		if x0 > cw-vw {
			x0 = cw - vw
		}
	}

	if vh < ch {
		if cursorY >= vh {
			y0 = cursorY - vh + 1
		}
		if y0 > ch-vh {
			y0 = ch - vh
		}
	}

	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	return x0, y0
}

// ViewportOriginForSnapshot resolves the current live viewport origin for the
// snapshot and viewport dimensions using the snapshot cursor.
func ViewportOriginForSnapshot(snap *protocolpb.Snapshot, viewCols, viewRows int) (int, int) {
	if snap == nil {
		return 0, 0
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if cols <= 0 || rows <= 0 {
		return 0, 0
	}
	if viewCols <= 0 {
		viewCols = cols
	}
	if viewRows <= 0 {
		viewRows = rows
	}
	cursorX := int(snap.Cursor.GetX())
	cursorY := int(snap.Cursor.GetY())
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorY < 0 {
		cursorY = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= rows {
		cursorY = rows - 1
	}
	return ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
}
