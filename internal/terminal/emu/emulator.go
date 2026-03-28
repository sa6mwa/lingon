package emu

import (
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/terminal"
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
	appCursor   bool

	attr cellAttr

	parser parserState

	tabStops []bool

	g0LineDrawing bool
	g1LineDrawing bool
	useG1         bool

	pendingGrapheme string
	pendingRune     rune
	pendingAttr     cellAttr
	pendingZWJ      bool

	scrollbackLimit   int
	scrollback        []terminal.ScrollbackRow
	scrollbackPending []terminal.ScrollbackRow

	cursorTrace      func(CursorTraceEvent)
	cursorTraceBytes []byte
	lastSavedReason  string

	eventTrace func(Event)

	inlineOriginRow int
	inlineOriginSet bool
}

type cellAttr struct {
	mode int16
	fg   uint32
	bg   uint32
}

const cursorTraceLimit = 64

// CursorTraceEvent describes a cursor move used for debugging.
type CursorTraceEvent struct {
	Reason string
	Old    terminal.Cursor
	New    terminal.Cursor
	Screen string
	Recent []byte
}

// Event describes a terminal control event and its effect on emulator state.
type Event struct {
	Name       string
	Final      byte
	Private    byte
	Params     []int
	Old        State
	New        State
	Screen     string
	ArgRow     int
	ArgCode    int
	PayloadLen int
}

// State captures cursor-relevant emulator state for tracing.
type State struct {
	Cursor          terminal.Cursor
	SavedCursor     terminal.Cursor
	ScrollTop       int
	ScrollBottom    int
	Cols            int
	Rows            int
	OriginMode      bool
	WrapMode        bool
	InsertMode      bool
	NewLineMode     bool
	AppCursorMode   bool
	CursorVisible   bool
	InlineOriginRow int
	InlineOriginSet bool
	AltScreen       bool
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

// SetCursorTrace installs a debug hook for cursor moves.
func (e *Emulator) SetCursorTrace(fn func(CursorTraceEvent)) {
	e.cursorTrace = fn
}

// SetEventTrace installs a debug hook for emulator control events.
func (e *Emulator) SetEventTrace(fn func(Event)) {
	e.eventTrace = fn
}

// SetInlineOriginRow configures a viewport-relative row offset for cursor positioning.
func (e *Emulator) SetInlineOriginRow(row int) {
	old := e.snapshotState()
	if row < 1 {
		e.inlineOriginRow = 0
		e.inlineOriginSet = false
		e.traceEvent("INLINE_ORIGIN", 0, 0, nil, old, row)
		return
	}
	e.inlineOriginRow = row
	e.inlineOriginSet = true
	e.traceEvent("INLINE_ORIGIN", 0, 0, nil, old, row)
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
	e.flushPendingGrapheme()
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
	e.flushPendingGrapheme()
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

// AltScreenActive reports whether the emulator is currently using the alternate screen.
func (e *Emulator) AltScreenActive() bool {
	return e.scr == &e.alt
}

func (e *Emulator) modeFlags() uint32 {
	var flags uint32
	if e.wrapMode {
		flags |= terminal.SnapshotModeWrap
	}
	if e.originMode {
		flags |= terminal.SnapshotModeOrigin
	}
	if e.insertMode {
		flags |= terminal.SnapshotModeInsert
	}
	if e.scr == &e.alt {
		flags |= terminal.SnapshotModeAltScreen
	}
	if e.appCursor {
		flags |= terminal.SnapshotModeAppCursor
	}
	return flags
}

func (e *Emulator) consumeByte(b byte) {
	e.recordTraceByte(b)
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

func (e *Emulator) recordTraceByte(b byte) {
	if e.cursorTrace == nil {
		return
	}
	if len(e.cursorTraceBytes) >= cursorTraceLimit {
		copy(e.cursorTraceBytes, e.cursorTraceBytes[1:])
		e.cursorTraceBytes = e.cursorTraceBytes[:cursorTraceLimit-1]
	}
	e.cursorTraceBytes = append(e.cursorTraceBytes, b)
}

func (e *Emulator) traceCursor(reason string, old, next terminal.Cursor) {
	e.traceCursorWithScreen(reason, old, next, "")
}

func (e *Emulator) traceCursorWithScreen(reason string, old, next terminal.Cursor, screen string) {
	if e.cursorTrace == nil {
		return
	}
	if old == next {
		return
	}
	if next.X != 0 || next.Y != 0 {
		return
	}
	recent := make([]byte, len(e.cursorTraceBytes))
	copy(recent, e.cursorTraceBytes)
	if screen == "" {
		if e.scr == &e.alt {
			screen = "alt"
		} else {
			screen = "main"
		}
	}
	e.cursorTrace(CursorTraceEvent{
		Reason: reason,
		Old:    old,
		New:    next,
		Screen: screen,
		Recent: recent,
	})
}

func (e *Emulator) snapshotState() State {
	state := State{
		Cursor:          e.scr.cursor,
		SavedCursor:     e.scr.savedCursor,
		ScrollTop:       e.scr.scrollTop,
		ScrollBottom:    e.scr.scrollBottom,
		Cols:            e.cols,
		Rows:            e.rows,
		OriginMode:      e.originMode,
		WrapMode:        e.wrapMode,
		InsertMode:      e.insertMode,
		NewLineMode:     e.newLineMode,
		AppCursorMode:   e.appCursor,
		CursorVisible:   e.cursorVisible,
		InlineOriginRow: e.inlineOriginRow,
		InlineOriginSet: e.inlineOriginSet,
		AltScreen:       e.scr == &e.alt,
	}
	return state
}

func (e *Emulator) traceEvent(name string, final byte, private byte, params []int, old State, argRow int) {
	e.traceEventWithDetails(name, final, private, params, old, argRow, 0, 0)
}

func (e *Emulator) traceEventWithDetails(name string, final byte, private byte, params []int, old State, argRow, argCode, payloadLen int) {
	if e.eventTrace == nil {
		return
	}
	next := e.snapshotState()
	dup := make([]int, len(params))
	copy(dup, params)
	screen := "main"
	if e.scr == &e.alt {
		screen = "alt"
	}
	e.eventTrace(Event{
		Name:       name,
		Final:      final,
		Private:    private,
		Params:     dup,
		Old:        old,
		New:        next,
		Screen:     screen,
		ArgRow:     argRow,
		ArgCode:    argCode,
		PayloadLen: payloadLen,
	})
}

func (e *Emulator) setCursor(reason string, x, y int) {
	old := e.scr.cursor
	e.scr.cursor.X = x
	e.scr.cursor.Y = y
	e.traceCursor(reason, old, e.scr.cursor)
}

func (e *Emulator) saveCursor(reason string) {
	e.scr.savedCursor = e.scr.cursor
	e.lastSavedReason = reason
}

func (e *Emulator) restoreCursor(reason string) {
	old := e.scr.cursor
	e.scr.cursor = e.scr.savedCursor
	if reason == "" && e.lastSavedReason != "" {
		reason = "RESTORE(" + e.lastSavedReason + ")"
	}
	e.traceCursor(reason, old, e.scr.cursor)
	e.wrapPending = false
}

func (e *Emulator) handleGround(b byte) {
	if len(e.parser.utf8Buf) > 0 {
		e.handlePrintableByte(b)
		return
	}
	if b == 0x1b { // ESC
		e.flushPendingGrapheme()
		e.parser.state = stateEscape
		return
	}
	if b == 0x9b { // CSI
		e.flushPendingGrapheme()
		e.parser.resetCSI()
		e.parser.state = stateCSI
		return
	}
	if b == 0x9d { // OSC
		e.flushPendingGrapheme()
		e.parser.resetOSC()
		e.parser.state = stateOSC
		return
	}
	if b < 0x20 || b == 0x7f {
		e.flushPendingGrapheme()
		e.handleControl(b)
		return
	}
	e.handlePrintableByte(b)
}

func (e *Emulator) handleEscape(b byte) {
	e.parser.state = stateGround
	old := e.snapshotState()
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
		e.saveCursor("ESC7")
		e.traceEvent("ESC7", b, 0, nil, old, 0)
	case '8':
		e.restoreCursor("ESC8")
		e.traceEvent("ESC8", b, 0, nil, old, 0)
	case 'D':
		e.index()
		e.traceEvent("IND", b, 0, nil, old, 0)
	case 'M':
		e.reverseIndex()
		e.traceEvent("RI", b, 0, nil, old, 0)
	case 'E':
		e.newLine(true)
		e.traceEvent("NEL", b, 0, nil, old, 0)
	case 'c':
		e.reset()
		e.traceEvent("RIS", b, 0, nil, old, 0)
	case 'H':
		e.setTabStop()
		e.traceEvent("HTS", b, 0, nil, old, 0)
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
	if (b == '?' || b == '>' || b == '<' || b == '=' || b == '!') && !e.parser.paramSeen && e.parser.private == 0 {
		e.parser.private = b
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
	e.flushPendingGrapheme()
	old := e.snapshotState()
	switch b {
	case 0x07: // BEL
		e.traceEvent("BEL", b, 0, nil, old, 0)
	case 0x08: // BS
		e.moveCursor(-1, 0)
		e.traceEvent("BS", b, 0, nil, old, 0)
	case 0x09: // TAB
		e.tab()
		e.traceEvent("TAB", b, 0, nil, old, 0)
	case 0x0a, 0x0b: // LF, VT
		e.newLine(false)
		e.traceEvent("LF", b, 0, nil, old, 0)
	case 0x0c: // FF (form feed)
		e.eraseDisplay(2)
		e.setCursor("FF", 0, 0)
		e.traceEvent("FF", b, 0, nil, old, 0)
	case 0x0d: // CR
		e.setCursor("CR", 0, e.scr.cursor.Y)
		e.traceEvent("CR", b, 0, nil, old, 0)
	case 0x0e: // SO
		e.useG1 = true
		e.traceEvent("SO", b, 0, nil, old, 0)
	case 0x0f: // SI
		e.useG1 = false
		e.traceEvent("SI", b, 0, nil, old, 0)
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
	old := e.snapshotState()
	code, payload := parseOSC(e.parser.oscBuf)
	if code == 0 || code == 2 {
		e.title = payload
	}
	e.traceEventWithDetails("OSC", 0, 0, nil, old, 0, code, len(payload))
	e.parser.resetOSC()
}

func (e *Emulator) handleCharsetByte(b byte) {
	old := e.snapshotState()
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
	e.traceEventWithDetails("CHARSET", b, 0, nil, old, 0, int(b), 0)
	e.parser.state = stateGround
}

func (e *Emulator) handleCSI(final byte, params []int, private byte) {
	old := e.snapshotState()
	switch final {
	case 'A':
		e.cursorUp(param(params, 0, 1))
		e.traceEvent("CUU", final, private, params, old, 0)
	case 'B':
		e.cursorDown(param(params, 0, 1))
		e.traceEvent("CUD", final, private, params, old, 0)
	case 'C':
		e.cursorForward(param(params, 0, 1))
		e.traceEvent("CUF", final, private, params, old, 0)
	case 'D':
		e.cursorBackward(param(params, 0, 1))
		e.traceEvent("CUB", final, private, params, old, 0)
	case 'E':
		e.cursorDown(param(params, 0, 1))
		e.setCursor("CNL", 0, e.scr.cursor.Y)
		e.traceEvent("CNL", final, private, params, old, 0)
	case 'F':
		e.cursorUp(param(params, 0, 1))
		e.setCursor("CPL", 0, e.scr.cursor.Y)
		e.traceEvent("CPL", final, private, params, old, 0)
	case 'G':
		e.cursorHorizontal(param(params, 0, 1))
		e.traceEvent("CHA", final, private, params, old, 0)
	case 'H', 'f':
		row := param(params, 0, 1)
		col := param(params, 1, 1)
		e.cursorPositionWithReason(row, col, "CUP")
		e.traceEvent("CUP", final, private, params, old, row)
	case 'J':
		e.eraseDisplay(param(params, 0, 0))
		e.traceEvent("ED", final, private, params, old, 0)
	case 'K':
		e.eraseLine(param(params, 0, 0))
		e.traceEvent("EL", final, private, params, old, 0)
	case 'L':
		e.insertLines(param(params, 0, 1))
		e.traceEvent("IL", final, private, params, old, 0)
	case 'M':
		e.deleteLines(param(params, 0, 1))
		e.traceEvent("DL", final, private, params, old, 0)
	case '@':
		e.insertChars(param(params, 0, 1))
		e.traceEvent("ICH", final, private, params, old, 0)
	case 'P':
		e.deleteChars(param(params, 0, 1))
		e.traceEvent("DCH", final, private, params, old, 0)
	case 'X':
		e.eraseChars(param(params, 0, 1))
		e.traceEvent("ECH", final, private, params, old, 0)
	case 'S':
		e.scrollUp(param(params, 0, 1))
		e.traceEvent("SU", final, private, params, old, 0)
	case 'T':
		e.scrollDown(param(params, 0, 1))
		e.traceEvent("SD", final, private, params, old, 0)
	case 'm':
		e.selectGraphicRendition(params)
		e.traceEvent("SGR", final, private, params, old, 0)
	case 'r':
		e.setScrollRegion(params)
		e.traceEvent("DECSTBM", final, private, params, old, 0)
	case 's':
		if private == 0 {
			e.saveCursor("CSI s")
			e.traceEvent("SCOSC", final, private, params, old, 0)
		} else {
			e.traceEvent("CSI_PRIVATE_UNHANDLED", final, private, params, old, 0)
		}
	case 'u':
		if private == 0 {
			e.restoreCursor("CSI u")
			e.traceEvent("SCORC", final, private, params, old, 0)
		} else {
			e.traceEvent("CSI_PRIVATE_UNHANDLED", final, private, params, old, 0)
		}
	case 'g':
		e.clearTabStops(param(params, 0, 0))
		e.traceEvent("TBC", final, private, params, old, 0)
	case 'h':
		if private == 0 || private == '?' {
			e.setMode(params, private, true)
			e.traceEvent("SM", final, private, params, old, 0)
		} else {
			e.traceEvent("CSI_PRIVATE_UNHANDLED", final, private, params, old, 0)
		}
	case 'l':
		if private == 0 || private == '?' {
			e.setMode(params, private, false)
			e.traceEvent("RM", final, private, params, old, 0)
		} else {
			e.traceEvent("CSI_PRIVATE_UNHANDLED", final, private, params, old, 0)
		}
	case 'd':
		row := param(params, 0, 1)
		e.cursorPositionWithReason(row, e.scr.cursor.X+1, "VPA")
		e.traceEvent("VPA", final, private, params, old, row)
	case 'e':
		e.cursorDown(param(params, 0, 1))
		e.traceEvent("VPR", final, private, params, old, 0)
	default:
		e.traceEvent("CSI_UNHANDLED", final, private, params, old, 0)
	}
}

func (e *Emulator) printRune(r rune) {
	r = e.translateRune(r)
	if e.pendingGrapheme != "" {
		if e.shouldExtendCluster(r) {
			e.pendingGrapheme += string(r)
			if r == zwjRune {
				e.pendingZWJ = true
			} else if e.pendingZWJ {
				e.pendingZWJ = false
			}
			return
		}
		e.flushPendingGrapheme()
	}
	e.pendingGrapheme = string(r)
	e.pendingRune = r
	e.pendingAttr = e.attr
	e.pendingZWJ = r == zwjRune
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

func (e *Emulator) setCell(x, y int, r rune, grapheme string, width int) {
	if !e.scr.inBounds(x, y) {
		return
	}
	idx := e.scr.index(x, y)
	e.scr.cells[idx] = terminal.Cell{
		Rune:     r,
		Grapheme: grapheme,
		Mode:     e.attr.mode,
		FG:       e.attr.fg,
		BG:       e.attr.bg,
	}
	if width == 2 && x+1 < e.cols {
		contIdx := e.scr.index(x+1, y)
		e.scr.cells[contIdx] = terminal.Cell{
			Rune: 0,
			Mode: e.attr.mode,
			FG:   e.attr.fg,
			BG:   e.attr.bg,
		}
	}
}

func (e *Emulator) shouldExtendCluster(r rune) bool {
	if isCombining(r) || isVariationSelector(r) || r == zwjRune {
		return true
	}
	return e.pendingZWJ
}

func (e *Emulator) flushPendingGrapheme() {
	if e.pendingGrapheme == "" {
		return
	}
	attr := e.attr
	e.attr = e.pendingAttr
	e.renderGrapheme(e.pendingGrapheme, e.pendingRune)
	e.attr = attr
	e.pendingGrapheme = ""
	e.pendingRune = 0
	e.pendingZWJ = false
}

func (e *Emulator) renderGrapheme(grapheme string, base rune) {
	if e.wrapPending {
		e.wrapPending = false
		e.newLine(true)
	}

	width := runewidth.StringWidth(grapheme)
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

	stored := grapheme
	if utf8.RuneCountInString(grapheme) == 1 {
		stored = ""
	}
	e.setCell(e.scr.cursor.X, e.scr.cursor.Y, base, stored, width)

	old := e.scr.cursor
	e.scr.cursor.X += width
	if e.scr.cursor.X >= e.cols {
		if e.wrapMode {
			e.wrapPending = true
			e.scr.cursor.X = e.cols - 1
		} else {
			e.scr.cursor.X = e.cols - 1
		}
	}
	e.traceCursor("PRINT", old, e.scr.cursor)
}

const zwjRune = '\u200d'

func isCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r)
}

func isVariationSelector(r rune) bool {
	if r >= 0xfe00 && r <= 0xfe0f {
		return true
	}
	return r >= 0xe0100 && r <= 0xe01ef
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
	old := e.scr.cursor
	e.scr.cursor.X = next
	e.traceCursor("TAB", old, e.scr.cursor)
}

func (e *Emulator) cursorPositionWithReason(row, col int, reason string) {
	if e.inlineOriginSet {
		row += e.inlineOriginRow - 1
	}
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
	e.setCursor(reason, x, clamp(y, 0, e.rows-1))
	e.wrapPending = false
}

func (e *Emulator) cursorHorizontal(col int) {
	if col < 1 {
		col = 1
	}
	if col > e.cols {
		col = e.cols
	}
	e.setCursor("CHA", col-1, e.scr.cursor.Y)
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
	old := e.scr.cursor
	e.scr.cursor.Y -= n
	if e.scr.cursor.Y < minY {
		e.scr.cursor.Y = minY
	}
	e.traceCursor("CUU", old, e.scr.cursor)
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
	old := e.scr.cursor
	e.scr.cursor.Y += n
	if e.scr.cursor.Y > maxY {
		e.scr.cursor.Y = maxY
	}
	e.traceCursor("CUD", old, e.scr.cursor)
	e.wrapPending = false
}

func (e *Emulator) cursorForward(n int) {
	if n < 1 {
		n = 1
	}
	old := e.scr.cursor
	e.scr.cursor.X += n
	if e.scr.cursor.X >= e.cols {
		e.scr.cursor.X = e.cols - 1
	}
	e.traceCursor("CUF", old, e.scr.cursor)
	e.wrapPending = false
}

func (e *Emulator) cursorBackward(n int) {
	if n < 1 {
		n = 1
	}
	old := e.scr.cursor
	e.scr.cursor.X -= n
	if e.scr.cursor.X < 0 {
		e.scr.cursor.X = 0
	}
	e.traceCursor("CUB", old, e.scr.cursor)
	e.wrapPending = false
}

func (e *Emulator) moveCursor(dx, dy int) {
	old := e.scr.cursor
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
	e.traceCursor("MOVE", old, e.scr.cursor)
	e.wrapPending = false
}

func (e *Emulator) newLine(withCR bool) {
	old := e.scr.cursor
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
	e.traceCursor("NL", old, e.scr.cursor)
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
	old := e.scr.cursor
	e.scr.cursor.Y--
	e.traceCursor("RI", old, e.scr.cursor)
}

func (e *Emulator) scrollUp(n int) {
	if n < 1 {
		n = 1
	}
	e.captureScrollback(n)
	e.scr.scrollUp(n, e.eraseCell())
}

func (e *Emulator) scrollDown(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.scrollDown(n, e.eraseCell())
}

// SetScrollbackLimit configures the max scrollback rows to retain.
func (e *Emulator) SetScrollbackLimit(lines int) {
	if lines < 0 {
		lines = 0
	}
	e.scrollbackLimit = lines
	if lines == 0 {
		e.clearScrollback()
		return
	}
	if extra := len(e.scrollback) - lines; extra > 0 {
		e.scrollback = e.scrollback[extra:]
	}
}

// ScrollbackSnapshot returns a copy of the current scrollback buffer.
func (e *Emulator) ScrollbackSnapshot() []terminal.ScrollbackRow {
	if len(e.scrollback) == 0 {
		return nil
	}
	out := make([]terminal.ScrollbackRow, len(e.scrollback))
	for i, row := range e.scrollback {
		cells := make([]terminal.Cell, len(row.Cells))
		copy(cells, row.Cells)
		out[i] = terminal.ScrollbackRow{Cols: row.Cols, Cells: cells}
	}
	return out
}

// DrainScrollback returns newly appended scrollback rows since the last drain.
func (e *Emulator) DrainScrollback() []terminal.ScrollbackRow {
	if len(e.scrollbackPending) == 0 {
		return nil
	}
	out := make([]terminal.ScrollbackRow, len(e.scrollbackPending))
	copy(out, e.scrollbackPending)
	e.scrollbackPending = nil
	return out
}

func (e *Emulator) clearScrollback() {
	e.scrollback = nil
	e.scrollbackPending = nil
}

func (e *Emulator) captureScrollback(n int) {
	if e.scrollbackLimit <= 0 {
		return
	}
	if e.scr != &e.main {
		return
	}
	top := e.scr.scrollTop
	if top != 0 {
		return
	}
	bottom := e.scr.scrollBottom
	if bottom >= e.rows {
		bottom = e.rows - 1
	}
	if top > bottom {
		return
	}
	height := bottom - top + 1
	if n > height {
		n = height
	}
	for i := 0; i < n; i++ {
		cells := e.scr.copyRowCells(top + i)
		row := terminal.ScrollbackRow{
			Cols:  e.cols,
			Cells: cells,
		}
		e.scrollback = append(e.scrollback, row)
		e.scrollbackPending = append(e.scrollbackPending, row)
	}
	if extra := len(e.scrollback) - e.scrollbackLimit; extra > 0 {
		e.scrollback = e.scrollback[extra:]
	}
	if len(e.scrollbackPending) > e.scrollbackLimit {
		e.scrollbackPending = e.scrollbackPending[len(e.scrollbackPending)-e.scrollbackLimit:]
	}
}

func (e *Emulator) eraseDisplay(mode int) {
	switch mode {
	case 0:
		e.eraseLine(0)
		for y := e.scr.cursor.Y + 1; y < e.rows; y++ {
			e.scr.clearLine(y, 0, e.cols-1, e.eraseCell())
		}
	case 1:
		for y := 0; y < e.scr.cursor.Y; y++ {
			e.scr.clearLine(y, 0, e.cols-1, e.eraseCell())
		}
		e.eraseLine(1)
	case 2:
		e.scr.clearAll(e.eraseCell())
	}
}

func (e *Emulator) eraseLine(mode int) {
	switch mode {
	case 0:
		e.scr.clearLine(e.scr.cursor.Y, e.scr.cursor.X, e.cols-1, e.eraseCell())
	case 1:
		e.scr.clearLine(e.scr.cursor.Y, 0, e.scr.cursor.X, e.eraseCell())
	case 2:
		e.scr.clearLine(e.scr.cursor.Y, 0, e.cols-1, e.eraseCell())
	}
}

func (e *Emulator) insertLines(n int) {
	if e.scr.cursor.Y < e.scr.scrollTop || e.scr.cursor.Y > e.scr.scrollBottom {
		return
	}
	if n < 1 {
		n = 1
	}
	e.scr.insertLines(e.scr.cursor.Y, n, e.eraseCell())
}

func (e *Emulator) deleteLines(n int) {
	if e.scr.cursor.Y < e.scr.scrollTop || e.scr.cursor.Y > e.scr.scrollBottom {
		return
	}
	if n < 1 {
		n = 1
	}
	e.scr.deleteLines(e.scr.cursor.Y, n, e.eraseCell())
}

func (e *Emulator) insertChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.insertChars(e.scr.cursor.Y, e.scr.cursor.X, n, e.eraseCell())
}

func (e *Emulator) deleteChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.deleteChars(e.scr.cursor.Y, e.scr.cursor.X, n, e.eraseCell())
}

func (e *Emulator) eraseChars(n int) {
	if n < 1 {
		n = 1
	}
	e.scr.clearLine(e.scr.cursor.Y, e.scr.cursor.X, e.scr.cursor.X+n-1, e.eraseCell())
}

func (e *Emulator) setScrollRegion(params []int) {
	top := param(params, 0, 1)
	bottom := param(params, 1, e.rows)
	if e.inlineOriginSet {
		top += e.inlineOriginRow - 1
		bottom += e.inlineOriginRow - 1
	}
	top--
	bottom--
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
	e.cursorPositionWithReason(1, 1, "DECSTBM")
}

func (e *Emulator) setMode(params []int, private byte, enable bool) {
	if private == '?' {
		for _, p := range params {
			switch p {
			case 7:
				e.wrapMode = enable
			case 25:
				e.cursorVisible = enable
			case 6:
				e.originMode = enable
				e.cursorPositionWithReason(1, 1, "DECOM")
			case 1:
				e.appCursor = enable
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
		e.alt.clearAll(e.eraseCell())
		e.scr = &e.alt
		e.setCursor("ALTSCREEN", 0, 0)
	} else {
		if saveCursor {
			old := e.main.cursor
			e.main.restoreCursor()
			e.traceCursorWithScreen("ALTRESTORE", old, e.main.cursor, "main")
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

func (e *Emulator) eraseCell() terminal.Cell {
	return terminal.Cell{
		Rune: ' ',
		Mode: 0,
		FG:   terminal.ColorDefault,
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
	e.inlineOriginRow = 0
	e.inlineOriginSet = false
	e.main.clearAll(e.eraseCell())
	e.alt.clearAll(e.eraseCell())
	e.scr = &e.main
	e.setCursor("RESET", 0, 0)
	e.scr.scrollTop = 0
	e.scr.scrollBottom = e.rows - 1
	e.tabStops = defaultTabs(e.cols)
}

func (e *Emulator) ensureCursorInBounds() {
	old := e.scr.cursor
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
	e.traceCursor("CLAMP", old, e.scr.cursor)
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
