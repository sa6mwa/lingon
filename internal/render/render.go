package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

const (
	ansiClearScreen = "\x1b[2J"
	ansiHome        = "\x1b[H"
	ansiHideCursor  = "\x1b[?25l"
	ansiShowCursor  = "\x1b[?25h"
	ansiReset       = "\x1b[0m"
)

// Snapshot renders a snapshot to the writer using ANSI escapes.
func Snapshot(w io.Writer, snap *protocolpb.Snapshot) error {
	if snap == nil {
		return nil
	}
	return SnapshotViewport(w, snap, int(snap.Cols), int(snap.Rows))
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

	x0, y0 := viewportOrigin(cols, rows, viewCols, viewRows, cursorX, cursorY)

	current := renderAttr{mode: -1, fg: ^uint32(0), bg: ^uint32(0)}
	defaultAttr := renderAttr{mode: 0, fg: terminal.ColorDefault, bg: terminal.ColorDefault}
	if _, err := io.WriteString(w, ansiReset); err != nil {
		return err
	}
	for y := 0; y < viewRows; y++ {
		cy := y0 + y
		if _, err := io.WriteString(w, fmt.Sprintf("\x1b[%d;%dH", y+1, 1)); err != nil {
			return err
		}
		var rowBuilder strings.Builder
		for x := 0; x < viewCols; x++ {
			cx := x0 + x
			attr := defaultAttr
			r := ' '
			if cx >= 0 && cy >= 0 && cx < cols && cy < rows {
				idx := cy*cols + cx
				if idx < len(snap.Runes) {
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
			}
			if r == 0 {
				r = ' '
			}
			if attr.mode&int32(terminal.ModeHidden) != 0 {
				r = ' '
			}
			if !attrEqual(current, attr) {
				rowBuilder.WriteString(sgr(attr))
				current = attr
			}
			rowBuilder.WriteRune(r)
		}
		if _, err := io.WriteString(w, rowBuilder.String()); err != nil {
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

type renderAttr struct {
	mode int32
	fg   uint32
	bg   uint32
}

func attrEqual(a, b renderAttr) bool {
	return a.mode == b.mode && a.fg == b.fg && a.bg == b.bg
}

func sgr(attr renderAttr) string {
	fg := attr.fg
	bg := attr.bg
	if attr.mode&int32(terminal.ModeInverse) != 0 {
		fg, bg = bg, fg
	}

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
	if attr.mode&int32(terminal.ModeInverse) != 0 {
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

func viewportOrigin(cw, ch, vw, vh, cursorX, cursorY int) (int, int) {
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
