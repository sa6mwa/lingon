package emu

import "pkt.systems/lingon/internal/terminal"

type screen struct {
	cols int
	rows int

	cells        []terminal.Cell
	cursor       terminal.Cursor
	savedCursor  terminal.Cursor
	scrollTop    int
	scrollBottom int
}

func newScreen(cols, rows int) screen {
	s := screen{
		cols:         cols,
		rows:         rows,
		cells:        make([]terminal.Cell, cols*rows),
		scrollTop:    0,
		scrollBottom: rows - 1,
	}
	s.clearAll(terminal.Cell{Rune: ' '})
	return s
}

func (s screen) resize(cols, rows int) screen {
	next := newScreen(cols, rows)
	minCols := cols
	if s.cols < minCols {
		minCols = s.cols
	}
	minRows := rows
	if s.rows < minRows {
		minRows = s.rows
	}
	for y := 0; y < minRows; y++ {
		for x := 0; x < minCols; x++ {
			next.cells[y*cols+x] = s.cells[y*s.cols+x]
		}
	}
	next.cursor = s.cursor
	if next.cursor.X >= cols {
		next.cursor.X = cols - 1
	}
	if next.cursor.Y >= rows {
		next.cursor.Y = rows - 1
	}
	next.savedCursor = s.savedCursor
	if next.savedCursor.X >= cols {
		next.savedCursor.X = cols - 1
	}
	if next.savedCursor.Y >= rows {
		next.savedCursor.Y = rows - 1
	}
	return next
}

func (s *screen) saveCursor() {
	s.savedCursor = s.cursor
}

func (s *screen) restoreCursor() {
	s.cursor = s.savedCursor
}

func (s *screen) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < s.cols && y < s.rows
}

func (s *screen) index(x, y int) int {
	return y*s.cols + x
}

func (s *screen) clearAll(fill terminal.Cell) {
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	for i := range s.cells {
		s.cells[i] = fill
	}
}

func (s *screen) clearLine(y, x0, x1 int, fill terminal.Cell) {
	if y < 0 || y >= s.rows {
		return
	}
	if x0 < 0 {
		x0 = 0
	}
	if x1 >= s.cols {
		x1 = s.cols - 1
	}
	if x0 > x1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	for x := x0; x <= x1; x++ {
		s.cells[s.index(x, y)] = fill
	}
}

func (s *screen) scrollUp(n int, fill terminal.Cell) {
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	top := s.scrollTop
	bottom := s.scrollBottom
	if top < 0 {
		top = 0
	}
	if bottom >= s.rows {
		bottom = s.rows - 1
	}
	height := bottom - top + 1
	if n > height {
		n = height
	}
	cols := s.cols
	copy(s.cells[top*cols:], s.cells[(top+n)*cols:(bottom+1)*cols])
	for y := bottom - n + 1; y <= bottom; y++ {
		for x := 0; x < cols; x++ {
			s.cells[s.index(x, y)] = fill
		}
	}
}

func (s *screen) scrollDown(n int, fill terminal.Cell) {
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	top := s.scrollTop
	bottom := s.scrollBottom
	if top < 0 {
		top = 0
	}
	if bottom >= s.rows {
		bottom = s.rows - 1
	}
	height := bottom - top + 1
	if n > height {
		n = height
	}
	cols := s.cols
	for y := bottom; y >= top+n; y-- {
		copy(s.cells[y*cols:(y+1)*cols], s.cells[(y-n)*cols:(y-n+1)*cols])
	}
	for y := top; y < top+n; y++ {
		for x := 0; x < cols; x++ {
			s.cells[s.index(x, y)] = fill
		}
	}
}

func (s *screen) insertLines(row, n int, fill terminal.Cell) {
	if row < s.scrollTop || row > s.scrollBottom {
		return
	}
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	if n > s.scrollBottom-row+1 {
		n = s.scrollBottom - row + 1
	}
	cols := s.cols
	for y := s.scrollBottom; y >= row+n; y-- {
		copy(s.cells[y*cols:(y+1)*cols], s.cells[(y-n)*cols:(y-n+1)*cols])
	}
	for y := row; y < row+n; y++ {
		for x := 0; x < cols; x++ {
			s.cells[s.index(x, y)] = fill
		}
	}
}

func (s *screen) deleteLines(row, n int, fill terminal.Cell) {
	if row < s.scrollTop || row > s.scrollBottom {
		return
	}
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	if n > s.scrollBottom-row+1 {
		n = s.scrollBottom - row + 1
	}
	cols := s.cols
	for y := row; y <= s.scrollBottom-n; y++ {
		copy(s.cells[y*cols:(y+1)*cols], s.cells[(y+n)*cols:(y+n+1)*cols])
	}
	for y := s.scrollBottom - n + 1; y <= s.scrollBottom; y++ {
		for x := 0; x < cols; x++ {
			s.cells[s.index(x, y)] = fill
		}
	}
}

func (s *screen) insertChars(row, col, n int, fill terminal.Cell) {
	if row < 0 || row >= s.rows {
		return
	}
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	if col < 0 {
		col = 0
	}
	if col >= s.cols {
		return
	}
	if n > s.cols-col {
		n = s.cols - col
	}
	start := s.index(col, row)
	end := s.index(s.cols-1, row) + 1
	copy(s.cells[start+n:end], s.cells[start:end-n])
	for x := col; x < col+n; x++ {
		s.cells[s.index(x, row)] = fill
	}
}

func (s *screen) deleteChars(row, col, n int, fill terminal.Cell) {
	if row < 0 || row >= s.rows {
		return
	}
	if n < 1 {
		return
	}
	if fill.Rune == 0 {
		fill.Rune = ' '
	}
	if col < 0 {
		col = 0
	}
	if col >= s.cols {
		return
	}
	if n > s.cols-col {
		n = s.cols - col
	}
	start := s.index(col, row)
	end := s.index(s.cols-1, row) + 1
	copy(s.cells[start:end-n], s.cells[start+n:end])
	for x := s.cols - n; x < s.cols; x++ {
		s.cells[s.index(x, row)] = fill
	}
}
