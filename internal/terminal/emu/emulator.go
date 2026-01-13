package emu

import (
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/terminal"
)

const (
	flagWrap      uint32 = 1 << 0
	flagOrigin    uint32 = 1 << 1
	flagInsert    uint32 = 1 << 2
	flagAltScreen uint32 = 1 << 3
)

// Emulator implements a minimal VT-style terminal emulator for Lingon.
type Emulator struct {
	cols int
	rows int

	main screen
	alt  screen
	scr  *screen

	cursorVisible bool
	title         string

	wrapPending bool
	wrapMode    bool
	originMode  bool
	insertMode  bool
	newLineMode bool

	attr cellAttr

	parser parserState

	tabStops []bool

	g0LineDrawing bool
	g1LineDrawing bool
	useG1         bool
}

type cellAttr struct {
	mode int16
	fg   uint32
	bg   uint32
}

// New constructs a new VT emulator with the given size.
func New(cols, rows int) *Emulator {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	e := &Emulator{
		cols:          cols,
		rows:          rows,
		cursorVisible: true,
		wrapMode:      true,
	}
	e.main = newScreen(cols, rows)
	e.alt = newScreen(cols, rows)
	e.scr = &e.main
	e.tabStops = defaultTabs(cols)
	e.resetAttributes()
	return e
}

// Write feeds terminal output into the emulator.
func (e *Emulator) Write(p []byte) error {
	for len(p) > 0 {
		b := p[0]
		p = p[1:]
		e.consumeByte(b)
	}
	return nil
}

// Resize changes the emulator size.
func (e *Emulator) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	e.cols = cols
	e.rows = rows
	e.main = e.main.resize(cols, rows)
	e.alt = e.alt.resize(cols, rows)
	if e.scr == &e.alt {
		e.scr = &e.alt
	} else {
		e.scr = &e.main
	}
	e.tabStops = defaultTabs(cols)
	e.ensureCursorInBounds()
}

// Snapshot captures the emulator state.
func (e *Emulator) Snapshot() (terminal.Snapshot, error) {
	cells := make([]terminal.Cell, len(e.scr.cells))
	copy(cells, e.scr.cells)
	return terminal.Snapshot{
		Cols:          e.cols,
		Rows:          e.rows,
		Cursor:        terminal.Cursor{X: e.scr.cursor.X, Y: e.scr.cursor.Y},
		CursorVisible: e.cursorVisible,
		Mode:          e.modeFlags(),
		Title:         e.title,
		Cells:         cells,
	}, nil
}

func (e *Emulator) modeFlags() uint32 {
	var flags uint32
	if e.wrapMode {
		flags |= flagWrap
	}
	if e.originMode {
		flags |= flagOrigin
	}
	if e.insertMode {
		flags |= flagInsert
	}
	if e.scr == &e.alt {
		flags |= flagAltScreen
	}
	return flags
}

func (e *Emulator) consumeByte(b byte) {
	switch e.parser.state {
	case stateGround:
		e.handleGround(b)
	case stateEscape:
		e.handleEscape(b)
	case stateCSI:
		e.handleCSIByte(b)
	case stateOSC:
		e.handleOSCByte(b)
	case stateString:
		e.handleStringByte(b)
	case stateCharset:
		e.handleCharsetByte(b)
	default:
		e.parser.state = stateGround
	}
}

func (e *Emulator) handleGround(b byte) {
	if b == 0x1b { // ESC
		e.parser.state = stateEscape
		return
	}
	if b == 0x9b { // CSI
		e.parser.resetCSI()
		e.parser.state = stateCSI
		return
	}
	if b == 0x9d { // OSC
		e.parser.resetOSC()
		e.parser.state = stateOSC
		return
	}
	if b < 0x20 || b == 0x7f {
		e.handleControl(b)
		return
	}
	e.handlePrintableByte(b)
}

func (e *Emulator) handleEscape(b byte) {
	e.parser.state = stateGround
	switch b {
	case '[':
		e.parser.resetCSI()
		e.parser.state = stateCSI
	case ']':
		e.parser.resetOSC()
		e.parser.state = stateOSC
	case 'P', 'X', '^', '_':
		e.parser.resetString()
		e.parser.state = stateString
	case '7':
		e.scr.saveCursor()
	case '8':
		e.scr.restoreCursor()
	case 'D':
		e.index()
	case 'M':
		e.reverseIndex()
	case 'E':
		e.newLine(true)
	case 'c':
		e.reset()
	case 'H':
		e.setTabStop()
	case '(':
		e.parser.charsetTarget = 0
		e.parser.state = stateCharset
	case ')':
		e.parser.charsetTarget = 1
		e.parser.state = stateCharset
	default:
		// Ignore unknown escape.
	}
}

func (e *Emulator) handleCSIByte(b byte) {
	if b >= 0x40 && b <= 0x7e {
		private := e.parser.private
		params := e.parser.finalizeParams()
		e.parser.state = stateGround
		e.handleCSI(b, params, private)
		return
	}
	if b == '?' && !e.parser.paramSeen {
		e.parser.private = true
		return
	}
	if b >= '0' && b <= '9' {
		e.parser.addDigit(int(b - '0'))
		return
	}
	if b == ';' {
		e.parser.nextParam()
		return
	}
	if b >= 0x20 && b <= 0x2f {
		return
	}
	if b == 0x1b {
		e.parser.state = stateEscape
		return
	}
}

func (e *Emulator) handleOSCByte(b byte) {
	if e.parser.oscEsc {
		e.parser.oscEsc = false
		if b == '\\' {
			e.parser.state = stateGround
			e.handleOSC()
			return
		}
		e.parser.oscBuf = append(e.parser.oscBuf, 0x1b, b)
		return
	}
	if b == 0x1b {
		e.parser.oscEsc = true
		return
	}
	if b == 0x07 {
		e.parser.state = stateGround
		e.handleOSC()
		return
	}
	e.parser.oscBuf = append(e.parser.oscBuf, b)
}

func (e *Emulator) handleStringByte(b byte) {
	if e.parser.oscEsc {
		e.parser.oscEsc = false
		if b == '\\' {
			e.parser.state = stateGround
			return
		}
		return
	}
	if b == 0x1b {
		e.parser.oscEsc = true
		return
	}
}

func (e *Emulator) handleControl(b byte) {
	switch b {
	case 0x07: // BEL
	case 0x08: // BS
		e.moveCursor(-1, 0)
	case 0x09: // TAB
		e.tab()
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		e.newLine(false)
	case 0x0d: // CR
		e.scr.cursor.X = 0
	case 0x0e: // SO
		e.useG1 = true
	case 0x0f: // SI
		e.useG1 = false
	default:
	}
}

func (e *Emulator) handlePrintableByte(b byte) {
	if b < utf8.RuneSelf {
		e.printRune(rune(b))
		return
	}
	e.parser.utf8Buf = append(e.parser.utf8Buf, b)
	if utf8.FullRune(e.parser.utf8Buf) {
		r, size := utf8.DecodeRune(e.parser.utf8Buf)
		e.parser.utf8Buf = e.parser.utf8Buf[:0]
		if r == utf8.RuneError && size == 1 {
			r = rune(b)
		}
		e.printRune(r)
	}
}

func (e *Emulator) handleOSC() {
	code, payload := parseOSC(e.parser.oscBuf)
	if code == 0 || code == 2 {
		e.title = payload
	}
	e.parser.resetOSC()
}

func (e *Emulator) handleCharsetByte(b byte) {
	switch e.parser.charsetTarget {
	case 0:
		e.g0LineDrawing = b == '0'
		if b == 'B' {
			e.g0LineDrawing = false
		}
	case 1:
		e.g1LineDrawing = b == '0'
		if b == 'B' {
			e.g1LineDrawing = false
		}
	}
	e.parser.state = stateGround
}

func (e *Emulator) handleCSI(final byte, params []int, private bool) {
	switch final {
	case 'A':
		e.cursorUp(param(params, 0, 1))
	case 'B':
		e.cursorDown(param(params, 0, 1))
	case 'C':
		e.cursorForward(param(params, 0, 1))
	case 'D':
		e.cursorBackward(param(params, 0, 1))
	case 'E':
		e.cursorDown(param(params, 0, 1))
		e.scr.cursor.X = 0
	case 'F':
		e.cursorUp(param(params, 0, 1))
		e.scr.cursor.X = 0
	case 'G':
		e.cursorHorizontal(param(params, 0, 1))
	case 'H', 'f':
		row := param(params, 0, 1)
		col := param(params, 1, 1)
		e.cursorPosition(row, col)
	case 'J':
		e.eraseDisplay(param(params, 0, 0))
	case 'K':
		e.eraseLine(param(params, 0, 0))
	case 'L':
		e.insertLines(param(params, 0, 1))
	case 'M':
		e.deleteLines(param(params, 0, 1))
	case '@':
		e.insertChars(param(params, 0, 1))
	case 'P':
		e.deleteChars(param(params, 0, 1))
	case 'X':
		e.eraseChars(param(params, 0, 1))
	case 'S':
		e.scrollUp(param(params, 0, 1))
	case 'T':
		e.scrollDown(param(params, 0, 1))
	case 'm':
		e.selectGraphicRendition(params)
	case 'r':
		e.setScrollRegion(params)
	case 's':
		e.scr.saveCursor()
	case 'u':
		e.scr.restoreCursor()
	case 'g':
		e.clearTabStops(param(params, 0, 0))
	case 'h':
		e.setMode(params, private, true)
	case 'l':
		e.setMode(params, private, false)
	case 'd':
		row := param(params, 0, 1)
		e.cursorPosition(row, e.scr.cursor.X+1)
	case 'e':
		e.cursorDown(param(params, 0, 1))
	}
}

func (e *Emulator) printRune(r rune) {
	r = e.translateRune(r)
	if e.wrapPending {
		e.wrapPending = false
		e.newLine(true)
	}

	width := runewidth.RuneWidth(r)
	if width <= 0 {
		width = 1
	}
	if width > e.cols {
		width = 1
	}

	if e.scr.cursor.X >= e.cols {
		if e.wrapMode {
			e.newLine(true)
		}
	}

	if width == 2 && e.scr.cursor.X == e.cols-1 {
		if e.wrapMode {
			e.newLine(true)
		}
	}

	if e.insertMode {
		e.insertChars(width)
	}

	e.setCell(e.scr.cursor.X, e.scr.cursor.Y, r, width)

	e.scr.cursor.X += width
	if e.scr.cursor.X >= e.cols {
		if e.wrapMode {
			e.wrapPending = true
			e.scr.cursor.X = e.cols - 1
		} else {
			e.scr.cursor.X = e.cols - 1
		}
	}
}

func (e *Emulator) translateRune(r rune) rune {
	if r < 0x20 || r > 0x7e {
		return r
	}
	lineDrawing := e.g0LineDrawing
	if e.useG1 {
		lineDrawing = e.g1LineDrawing
	}
	if !lineDrawing {
		return r
	}
	return mapLineDrawing(r)
}

func (e *Emulator) setCell(x, y int, r rune, width int) {
	if !e.scr.inBounds(x, y) {
		return
	}
	idx := e.scr.index(x, y)
	e.scr.cells[idx] = terminal.Cell{
		Rune: r,
		Mode: e.attr.mode,
		FG:   e.attr.fg,
		BG:   e.attr.bg,
	}
	if width == 2 && x+1 < e.cols {
		contIdx := e.scr.index(x+1, y)
		e.scr.cells[contIdx] = terminal.Cell{
			Rune: ' ',
			Mode: e.attr.mode,
			FG:   e.attr.fg,
			BG:   e.attr.bg,
		}
	}
}

func (e *Emulator) setTabStop() {
	if e.scr.cursor.X >= 0 && e.scr.cursor.X < len(e.tabStops) {
		e.tabStops[e.scr.cursor.X] = true
	}
}

func (e *Emulator) clearTabStops(mode int) {
	switch mode {
	case 0:
		if e.scr.cursor.X >= 0 && e.scr.cursor.X < len(e.tabStops) {
			e.tabStops[e.scr.cursor.X] = false
		}
	case 3:
		e.tabStops = make([]bool, e.cols)
	}
}

func (e *Emulator) tab() {
	next := e.cols - 1
	for i := e.scr.cursor.X + 1; i < len(e.tabStops); i++ {
		if e.tabStops[i] {
			next = i
			break
		}
	}
	e.scr.cursor.X = next
}

func (e *Emulator) cursorPosition(row, col int) {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	y := row - 1
	if e.originMode {
		y += e.scr.scrollTop
	}
	if y > e.scr.scrollBottom {
		y = e.scr.scrollBottom
	}
	x := col - 1
	if x >= e.cols {
		x = e.cols - 1
	}
	e.scr.cursor.X = x
	e.scr.cursor.Y = clamp(y, 0, e.rows-1)
	e.wrapPending = false
}

func (e *Emulator) cursorHorizontal(col int) {
	if col < 1 {
		col = 1
	}
	if col > e.cols {
		col = e.cols
	}
	e.scr.cursor.X = col - 1
	e.wrapPending = false
}

func (e *Emulator) cursorUp(n int) {
	if n < 1 {
		n = 1
	}
	minY := 0
	if e.originMode {
		minY = e.scr.scrollTop
	}
	e.scr.cursor.Y -= n
	if e.scr.cursor.Y < minY {
		e.scr.cursor.Y = minY
	}
	e.wrapPending = false
}

func (e *Emulator) cursorDown(n int) {
	if n < 1 {
		n = 1
	}
	maxY := e.rows - 1
	if e.originMode {
		maxY = e.scr.scrollBottom
	}
	e.scr.cursor.Y += n
	if e.scr.cursor.Y > maxY {
		e.scr.cursor.Y = maxY
	}
	e.wrapPending = false
}

func (e *Emulator) cursorForward(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.cursor.X += n
	if e.scr.cursor.X >= e.cols {
		e.scr.cursor.X = e.cols - 1
	}
	e.wrapPending = false
}

func (e *Emulator) cursorBackward(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.cursor.X -= n
	if e.scr.cursor.X < 0 {
		e.scr.cursor.X = 0
	}
	e.wrapPending = false
}

func (e *Emulator) moveCursor(dx, dy int) {
	e.scr.cursor.X += dx
	e.scr.cursor.Y += dy
	if e.scr.cursor.X < 0 {
		e.scr.cursor.X = 0
	}
	if e.scr.cursor.X >= e.cols {
		e.scr.cursor.X = e.cols - 1
	}
	if e.scr.cursor.Y < 0 {
		e.scr.cursor.Y = 0
	}
	if e.scr.cursor.Y >= e.rows {
		e.scr.cursor.Y = e.rows - 1
	}
	e.wrapPending = false
}

func (e *Emulator) newLine(withCR bool) {
	if withCR {
		e.scr.cursor.X = 0
	}
	e.scr.cursor.Y++
	if e.scr.cursor.Y > e.scr.scrollBottom {
		e.scr.cursor.Y = e.scr.scrollBottom
		e.scrollUp(1)
	}
	if e.newLineMode {
		e.scr.cursor.X = 0
	}
	e.wrapPending = false
}

func (e *Emulator) index() {
	e.newLine(false)
}

func (e *Emulator) reverseIndex() {
	if e.scr.cursor.Y == e.scr.scrollTop {
		e.scrollDown(1)
		return
	}
	e.scr.cursor.Y--
}

func (e *Emulator) scrollUp(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.scrollUp(n, e.blankCell())
}

func (e *Emulator) scrollDown(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.scrollDown(n, e.blankCell())
}

func (e *Emulator) eraseDisplay(mode int) {
	switch mode {
	case 0:
		e.eraseLine(0)
		for y := e.scr.cursor.Y + 1; y < e.rows; y++ {
			e.scr.clearLine(y, 0, e.cols-1, e.blankCell())
		}
	case 1:
		for y := 0; y < e.scr.cursor.Y; y++ {
			e.scr.clearLine(y, 0, e.cols-1, e.blankCell())
		}
		e.eraseLine(1)
	case 2:
		e.scr.clearAll(e.blankCell())
	}
}

func (e *Emulator) eraseLine(mode int) {
	switch mode {
	case 0:
		e.scr.clearLine(e.scr.cursor.Y, e.scr.cursor.X, e.cols-1, e.blankCell())
	case 1:
		e.scr.clearLine(e.scr.cursor.Y, 0, e.scr.cursor.X, e.blankCell())
	case 2:
		e.scr.clearLine(e.scr.cursor.Y, 0, e.cols-1, e.blankCell())
	}
}

func (e *Emulator) insertLines(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.insertLines(e.scr.cursor.Y, n, e.blankCell())
}

func (e *Emulator) deleteLines(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.deleteLines(e.scr.cursor.Y, n, e.blankCell())
}

func (e *Emulator) insertChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.insertChars(e.scr.cursor.Y, e.scr.cursor.X, n, e.blankCell())
}

func (e *Emulator) deleteChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.deleteChars(e.scr.cursor.Y, e.scr.cursor.X, n, e.blankCell())
}

func (e *Emulator) eraseChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.clearLine(e.scr.cursor.Y, e.scr.cursor.X, e.scr.cursor.X+n-1, e.blankCell())
}

func (e *Emulator) setScrollRegion(params []int) {
	top := param(params, 0, 1) - 1
	bottom := param(params, 1, e.rows) - 1
	if top < 0 {
		top = 0
	}
	if bottom >= e.rows {
		bottom = e.rows - 1
	}
	if top >= bottom {
		e.scr.scrollTop = 0
		e.scr.scrollBottom = e.rows - 1
	} else {
		e.scr.scrollTop = top
		e.scr.scrollBottom = bottom
	}
	e.cursorPosition(1, 1)
}

func (e *Emulator) setMode(params []int, private, enable bool) {
	if private {
		for _, p := range params {
			switch p {
			case 7:
				e.wrapMode = enable
			case 25:
				e.cursorVisible = enable
			case 6:
				e.originMode = enable
				e.cursorPosition(1, 1)
			case 47, 1047, 1049:
				e.setAltScreen(enable, p == 1049)
			}
		}
		return
	}
	for _, p := range params {
		switch p {
		case 4:
			e.insertMode = enable
		case 20:
			e.newLineMode = enable
		}
	}
}

func (e *Emulator) setAltScreen(enable bool, saveCursor bool) {
	if enable {
		if saveCursor {
			e.main.saveCursor()
		}
		e.alt.clearAll(e.blankCell())
		e.scr = &e.alt
		e.scr.cursor = terminal.Cursor{X: 0, Y: 0}
	} else {
		if saveCursor {
			e.main.restoreCursor()
		}
		e.scr = &e.main
	}
}

func (e *Emulator) selectGraphicRendition(params []int) {
	if len(params) == 0 {
		params = []int{0}
	} else {
		for i := range params {
			if params[i] == -1 {
				params[i] = 0
			}
		}
	}
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case 0:
			e.resetAttributes()
		case 1:
			e.attr.mode |= terminal.ModeBold
		case 2:
			e.attr.mode |= terminal.ModeFaint
		case 3:
			e.attr.mode |= terminal.ModeItalic
		case 4:
			e.attr.mode |= terminal.ModeUnderline
		case 5:
			e.attr.mode |= terminal.ModeBlink
		case 7:
			e.attr.mode |= terminal.ModeInverse
		case 8:
			e.attr.mode |= terminal.ModeHidden
		case 22:
			e.attr.mode &^= (terminal.ModeBold | terminal.ModeFaint)
		case 23:
			e.attr.mode &^= terminal.ModeItalic
		case 24:
			e.attr.mode &^= terminal.ModeUnderline
		case 25:
			e.attr.mode &^= terminal.ModeBlink
		case 27:
			e.attr.mode &^= terminal.ModeInverse
		case 28:
			e.attr.mode &^= terminal.ModeHidden
		case 39:
			e.attr.fg = terminal.ColorDefault
		case 49:
			e.attr.bg = terminal.ColorDefault
		default:
			if params[i] >= 30 && params[i] <= 37 {
				e.attr.fg = terminal.ColorIndexed | uint32(params[i]-30)
			} else if params[i] >= 40 && params[i] <= 47 {
				e.attr.bg = terminal.ColorIndexed | uint32(params[i]-40)
			} else if params[i] >= 90 && params[i] <= 97 {
				e.attr.fg = terminal.ColorIndexed | uint32(params[i]-90+8)
			} else if params[i] >= 100 && params[i] <= 107 {
				e.attr.bg = terminal.ColorIndexed | uint32(params[i]-100+8)
			} else if params[i] == 38 || params[i] == 48 {
				isFg := params[i] == 38
				if i+1 < len(params) && params[i+1] == 5 && i+2 < len(params) {
					if isFg {
						e.attr.fg = terminal.ColorIndexed256 | uint32(params[i+2])
					} else {
						e.attr.bg = terminal.ColorIndexed256 | uint32(params[i+2])
					}
					i += 2
				} else if i+1 < len(params) && params[i+1] == 2 && i+4 < len(params) {
					color := uint32(params[i+2])<<16 | uint32(params[i+3])<<8 | uint32(params[i+4])
					if isFg {
						e.attr.fg = terminal.ColorTrue | color
					} else {
						e.attr.bg = terminal.ColorTrue | color
					}
					i += 4
				}
			}
		}
	}
}

func (e *Emulator) blankCell() terminal.Cell {
	return terminal.Cell{
		Rune: ' ',
		Mode: e.attr.mode,
		FG:   e.attr.fg,
		BG:   e.attr.bg,
	}
}

func (e *Emulator) resetAttributes() {
	e.attr = cellAttr{
		mode: 0,
		fg:   terminal.ColorDefault,
		bg:   terminal.ColorDefault,
	}
}

func (e *Emulator) reset() {
	e.resetAttributes()
	e.wrapMode = true
	e.originMode = false
	e.insertMode = false
	e.newLineMode = false
	e.cursorVisible = true
	e.wrapPending = false
	e.title = ""
	e.main.clearAll(e.blankCell())
	e.alt.clearAll(e.blankCell())
	e.scr = &e.main
	e.scr.cursor = terminal.Cursor{}
	e.scr.scrollTop = 0
	e.scr.scrollBottom = e.rows - 1
	e.tabStops = defaultTabs(e.cols)
}

func (e *Emulator) ensureCursorInBounds() {
	if e.scr.cursor.X < 0 {
		e.scr.cursor.X = 0
	}
	if e.scr.cursor.X >= e.cols {
		e.scr.cursor.X = e.cols - 1
	}
	if e.scr.cursor.Y < 0 {
		e.scr.cursor.Y = 0
	}
	if e.scr.cursor.Y >= e.rows {
		e.scr.cursor.Y = e.rows - 1
	}
}

func defaultTabs(cols int) []bool {
	stops := make([]bool, cols)
	for i := 0; i < cols; i += 8 {
		stops[i] = true
	}
	return stops
}

func param(params []int, idx, def int) int {
	if idx >= len(params) {
		return def
	}
	if params[idx] < 0 {
		return def
	}
	if params[idx] == 0 {
		return def
	}
	return params[idx]
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func mapLineDrawing(r rune) rune {
	switch r {
	case '`':
		return '◆'
	case 'a':
		return '▒'
	case 'f':
		return '°'
	case 'g':
		return '±'
	case 'j':
		return '┘'
	case 'k':
		return '┐'
	case 'l':
		return '┌'
	case 'm':
		return '└'
	case 'n':
		return '┼'
	case 'q':
		return '─'
	case 't':
		return '├'
	case 'u':
		return '┤'
	case 'v':
		return '┴'
	case 'w':
		return '┬'
	case 'x':
		return '│'
	case '~':
		return '·'
	default:
		return r
	}
}
