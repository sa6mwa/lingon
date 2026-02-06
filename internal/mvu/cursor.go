package mvu

import (
	"bytes"
	"strconv"
)

const (
	ansiHideCursor = "\x1b[?25l"
	ansiShowCursor = "\x1b[?25h"
)

func writeCursor(w *bytes.Buffer, cursor Cursor) {
	if !cursor.Visible || cursor.Row <= 0 || cursor.Col <= 0 {
		w.WriteString(ansiHideCursor)
		return
	}
	w.WriteString(ansiShowCursor)
	w.WriteString("\x1b[")
	w.WriteString(strconv.Itoa(cursor.Row))
	w.WriteString(";")
	w.WriteString(strconv.Itoa(cursor.Col))
	w.WriteString("H")
}

// WriteCursor restores cursor visibility and position.
func WriteCursor(w *bytes.Buffer, cursor Cursor) {
	writeCursor(w, cursor)
}
