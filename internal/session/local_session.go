package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

type localSession struct {
	id   string
	name string

	shell string
	term  string

	cols            int
	rows            int
	scrollbackLines int

	respawnMu sync.RWMutex
	respawn   bool
	offlineMu sync.RWMutex
	offline   bool

	ptyMu   sync.RWMutex
	pty     *os.File
	tty     *os.File
	cmd     *exec.Cmd
	writeMu sync.Mutex

	emuMu    sync.Mutex
	emulator terminal.Emulator

	snapMu            sync.RWMutex
	snapshot          *protocolpb.Snapshot
	preserved         *protocolpb.Snapshot
	preserveOriginCol int
	preserveOriginRow int
	scrollMu          sync.RWMutex
	scroll            []terminal.ScrollbackRow
	pending           []terminal.ScrollbackRow

	holderMu sync.Mutex
	holderID string

	veofMu   sync.Mutex
	veofOrig uint8
	veofSet  bool

	lastActiveMu sync.RWMutex
	lastActive   time.Time

	publisher *host.Publisher
	closeOnce sync.Once

	logger pslog.Logger
	clock  clock.Clock
	trace  *trace.Writer

	allowRemoteResize bool

	ctx    context.Context
	cancel context.CancelFunc

	onOutput    func(id string, data []byte, snap *protocolpb.Snapshot)
	onPTYRead   func([]byte)
	onSnapshot  func(terminal.Snapshot)
	onExit      func(id string, err error)
	csiState    csiParser
	oscState    oscStreamParser
	cursorQuery func(terminal.Snapshot) (row, col int, ok bool)

	oscDefaultsMu    sync.RWMutex
	oscDefaultFg     string
	oscDefaultBg     string
	oscDefaultCursor string
	oscEchoState     oscEchoParser

	recentInputMu sync.Mutex
	recentInput   []byte

	remoteInputCh chan []byte
	outputNotify  chan struct{}

	resizeRedrawMu      sync.Mutex
	ignoreNextPTYOutput bool
}

var errPTYNotReady = errors.New("pty not initialized")

const recentInputLimit = 512
const remoteLineOutputTimeout = 40 * time.Millisecond

func (s *localSession) recordRecentInput(data []byte) {
	if len(data) == 0 {
		return
	}
	s.recentInputMu.Lock()
	defer s.recentInputMu.Unlock()
	if len(data) >= recentInputLimit {
		s.recentInput = append(s.recentInput[:0], data[len(data)-recentInputLimit:]...)
		return
	}
	if len(s.recentInput)+len(data) > recentInputLimit {
		trim := len(s.recentInput) + len(data) - recentInputLimit
		s.recentInput = append(s.recentInput[trim:], data...)
		return
	}
	s.recentInput = append(s.recentInput, data...)
}

func (s *localSession) recentInputSnapshot() []byte {
	s.recentInputMu.Lock()
	defer s.recentInputMu.Unlock()
	if len(s.recentInput) == 0 {
		return nil
	}
	out := make([]byte, len(s.recentInput))
	copy(out, s.recentInput)
	return out
}

func (s *localSession) resetOutputNotify() {
	if s == nil || s.outputNotify == nil {
		return
	}
	for {
		select {
		case <-s.outputNotify:
		default:
			return
		}
	}
}

func (s *localSession) notifyOutput() {
	if s == nil || s.outputNotify == nil {
		return
	}
	select {
	case s.outputNotify <- struct{}{}:
	default:
	}
}

func (s *localSession) waitForOutputWall(timeout time.Duration) bool {
	if s == nil || s.outputNotify == nil || timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-timer.C:
		return false
	case <-s.outputNotify:
		return true
	}
}

func (s *localSession) armIgnoreNextPTYOutput() {
	s.resizeRedrawMu.Lock()
	s.ignoreNextPTYOutput = true
	s.resizeRedrawMu.Unlock()
}

func (s *localSession) clearIgnoredPTYOutput() {
	s.resizeRedrawMu.Lock()
	s.ignoreNextPTYOutput = false
	s.resizeRedrawMu.Unlock()
}

func (s *localSession) shouldIgnoreNextPTYOutput(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	s.resizeRedrawMu.Lock()
	defer s.resizeRedrawMu.Unlock()
	if !s.ignoreNextPTYOutput {
		return false
	}
	if bytes.IndexByte(data, 0x1b) >= 0 {
		return true
	}
	if bytes.ContainsAny(data, "\r\n") {
		s.ignoreNextPTYOutput = false
		return false
	}
	s.ignoreNextPTYOutput = false
	return false
}

func (s *localSession) emuAltScreenActive() (bool, bool) {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	if s.emulator == nil {
		return false, false
	}
	type altChecker interface {
		AltScreenActive() bool
	}
	checker, ok := s.emulator.(altChecker)
	if !ok {
		return false, false
	}
	return checker.AltScreenActive(), true
}

func (s *localSession) setInlineOriginRow(row int) {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	if s.emulator == nil {
		return
	}
	type inlineOriginSetter interface {
		SetInlineOriginRow(int)
	}
	setter, ok := s.emulator.(inlineOriginSetter)
	if !ok {
		return
	}
	setter.SetInlineOriginRow(row)
}

func (s *localSession) loadEmulatorSnapshot(snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	if s.emulator == nil {
		return
	}
	type snapshotLoader interface {
		LoadSnapshot(terminal.Snapshot)
	}
	loader, ok := s.emulator.(snapshotLoader)
	if !ok {
		return
	}
	loader.LoadSnapshot(protocol.SnapshotFromProto(snap))
}

func inlineOriginRow(cursorRow, viewportRow, totalRows int) int {
	if viewportRow < 1 {
		return 0
	}
	if cursorRow < 0 {
		cursorRow = 0
	}
	if totalRows > 0 && cursorRow >= totalRows {
		cursorRow = totalRows - 1
	}
	origin := cursorRow - (viewportRow - 1) + 1
	if origin < 1 {
		return 1
	}
	return origin
}

type recentInputSignals struct {
	hasDSR          bool
	hasHome         bool
	hasClear        bool
	hasAltEnter     bool
	hasAltLeave     bool
	hasCUP          bool
	hasCUPHome      bool
	hasED           bool
	hasED2          bool
	hasScrollRegion bool
	hasRIS          bool
}

func recentInputHas(data []byte, seq string) bool {
	if len(data) == 0 || seq == "" {
		return false
	}
	return bytes.Contains(data, []byte(seq))
}

func csiParam(params []int, idx int, def int) int {
	if idx >= len(params) {
		return def
	}
	v := params[idx]
	if v < 0 {
		return def
	}
	return v
}

func analyzeRecentInput(data []byte) recentInputSignals {
	out := recentInputSignals{
		hasDSR:      recentInputHas(data, "\x1b[6n") || recentInputHas(data, "\x1b[?6n"),
		hasHome:     recentInputHas(data, "\x1b[H"),
		hasClear:    recentInputHas(data, "\x1b[2J"),
		hasAltEnter: recentInputHas(data, "\x1b[?1049h"),
		hasAltLeave: recentInputHas(data, "\x1b[?1049l"),
		hasRIS:      recentInputHas(data, "\x1bc"),
	}
	var p csiParser
	for _, b := range data {
		final, params, _, ok := p.Feed(byte(b))
		if !ok {
			continue
		}
		switch final {
		case 'H', 'f':
			out.hasCUP = true
			row := csiParam(params, 0, 1)
			col := csiParam(params, 1, 1)
			if row == 1 && col == 1 {
				out.hasCUPHome = true
			}
		case 'J':
			out.hasED = true
			if csiParam(params, 0, 0) == 2 {
				out.hasED2 = true
			}
		case 'r':
			out.hasScrollRegion = true
		}
	}
	return out
}

type localSessionOptions struct {
	ID                string
	Name              string
	Shell             string
	Term              string
	Cols              int
	Rows              int
	ScrollbackLines   int
	Respawn           bool
	Offline           bool
	Logger            pslog.Logger
	Clock             clock.Clock
	OnOutput          func(id string, data []byte, snap *protocolpb.Snapshot)
	OnPTYRead         func([]byte)
	OnSnapshot        func(terminal.Snapshot)
	OnExit            func(id string, err error)
	CursorQuery       func(terminal.Snapshot) (row, col int, ok bool)
	Trace             *trace.Writer
	DefaultFg         string
	DefaultBg         string
	DefaultCursor     string
	AllowRemoteResize bool
}

func newLocalSession(parent context.Context, opts localSessionOptions) *localSession {
	ctx, cancel := context.WithCancel(parent)
	logger := opts.Logger
	if logger == nil {
		logger = logging.Default()
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.New()
	}
	session := &localSession{
		id:                opts.ID,
		name:              opts.Name,
		shell:             opts.Shell,
		term:              opts.Term,
		cols:              opts.Cols,
		rows:              opts.Rows,
		scrollbackLines:   opts.ScrollbackLines,
		respawn:           opts.Respawn,
		offline:           opts.Offline,
		logger:            logger,
		clock:             clk,
		ctx:               ctx,
		cancel:            cancel,
		onOutput:          opts.OnOutput,
		onPTYRead:         opts.OnPTYRead,
		onSnapshot:        opts.OnSnapshot,
		onExit:            opts.OnExit,
		cursorQuery:       opts.CursorQuery,
		trace:             opts.Trace,
		allowRemoteResize: opts.AllowRemoteResize,
		remoteInputCh:     make(chan []byte, 256),
		outputNotify:      make(chan struct{}, 1),
	}
	if opts.Cols > 0 && opts.Rows > 0 {
		session.snapshot = &protocolpb.Snapshot{
			Cols:          uint32(opts.Cols),
			Rows:          uint32(opts.Rows),
			Runes:         make([]uint32, opts.Cols*opts.Rows),
			Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
			CursorVisible: true,
		}
		session.preserved = cloneSnapshot(session.snapshot)
	}
	session.setOscDefaults(opts.DefaultFg, opts.DefaultBg, opts.DefaultCursor)
	go session.runRemoteInput()
	return session
}

func (s *localSession) ID() string {
	return s.id
}

func (s *localSession) Name() string {
	return s.name
}

func (s *localSession) AllowRemoteResize() bool {
	return s != nil && s.allowRemoteResize
}

func (s *localSession) SetPublisher(p *host.Publisher) {
	s.publisher = p
	if p != nil {
		p.SetScrollbackSnapshot(s.scrollbackSnapshot)
		p.SetOffline(s.Offline())
	}
}

func (s *localSession) scrollbackSnapshot() []terminal.ScrollbackRow {
	s.scrollMu.RLock()
	defer s.scrollMu.RUnlock()
	if len(s.scroll) == 0 {
		return nil
	}
	out := make([]terminal.ScrollbackRow, len(s.scroll))
	for i, row := range s.scroll {
		cells := make([]terminal.Cell, len(row.Cells))
		copy(cells, row.Cells)
		out[i] = terminal.ScrollbackRow{Cols: row.Cols, Cells: cells}
	}
	return out
}

func (s *localSession) appendScrollback(rows []terminal.ScrollbackRow) []terminal.ScrollbackRow {
	if len(rows) == 0 {
		return nil
	}
	s.scrollMu.Lock()
	defer s.scrollMu.Unlock()
	appended := make([]terminal.ScrollbackRow, len(rows))
	for i, row := range rows {
		cells := make([]terminal.Cell, len(row.Cells))
		copy(cells, row.Cells)
		appended[i] = terminal.ScrollbackRow{Cols: row.Cols, Cells: cells}
	}
	s.scroll = append(s.scroll, appended...)
	if s.scrollbackLines > 0 && len(s.scroll) > s.scrollbackLines {
		extra := len(s.scroll) - s.scrollbackLines
		s.scroll = append([]terminal.ScrollbackRow(nil), s.scroll[extra:]...)
	}
	return appended
}

func cloneScrollbackRows(rows []terminal.ScrollbackRow) []terminal.ScrollbackRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]terminal.ScrollbackRow, len(rows))
	for i, row := range rows {
		cells := make([]terminal.Cell, len(row.Cells))
		copy(cells, row.Cells)
		out[i] = terminal.ScrollbackRow{Cols: row.Cols, Cells: cells}
	}
	return out
}

func snapshotToScrollbackRows(snap *protocolpb.Snapshot) []terminal.ScrollbackRow {
	if snap == nil {
		return nil
	}
	cols := int(snap.GetCols())
	rows := int(snap.GetRows())
	if cols <= 0 || rows <= 0 {
		return nil
	}
	out := make([]terminal.ScrollbackRow, 0, rows)
	for y := 0; y < rows; y++ {
		cells := make([]terminal.Cell, cols)
		rowStart := y * cols
		for x := 0; x < cols; x++ {
			idx := rowStart + x
			cell := terminal.Cell{}
			if idx < len(snap.Runes) {
				cell.Rune = rune(snap.Runes[idx])
			}
			if idx < len(snap.Modes) {
				cell.Mode = int16(snap.Modes[idx])
			}
			if idx < len(snap.Fg) {
				cell.FG = snap.Fg[idx]
			}
			if idx < len(snap.Bg) {
				cell.BG = snap.Bg[idx]
			}
			if idx < len(snap.Graphemes) {
				cell.Grapheme = snap.Graphemes[idx]
			}
			cells[x] = cell
		}
		out = append(out, terminal.ScrollbackRow{Cols: cols, Cells: cells})
	}
	return out
}

func (s *localSession) setPendingPreservedScrollbackFromSnapshot(snap *protocolpb.Snapshot) {
	rows := snapshotToScrollbackRows(snap)
	s.scrollMu.Lock()
	s.pending = rows
	s.scrollMu.Unlock()
}

func (s *localSession) drainScrollbackRows(drained []terminal.ScrollbackRow) []terminal.ScrollbackRow {
	if len(drained) == 0 {
		return nil
	}
	s.scrollMu.Lock()
	defer s.scrollMu.Unlock()
	if len(s.pending) == 0 {
		return cloneScrollbackRows(drained)
	}
	replace := len(drained)
	if replace > len(s.pending) {
		replace = len(s.pending)
	}
	out := make([]terminal.ScrollbackRow, 0, len(drained))
	out = append(out, cloneScrollbackRows(s.pending[:replace])...)
	out = append(out, cloneScrollbackRows(drained[replace:])...)
	s.pending = append([]terminal.ScrollbackRow(nil), s.pending[replace:]...)
	return out
}

func (s *localSession) SetLastActive(t time.Time) {
	s.lastActiveMu.Lock()
	s.lastActive = t
	s.lastActiveMu.Unlock()
}

func (s *localSession) LastActive() time.Time {
	s.lastActiveMu.RLock()
	defer s.lastActiveMu.RUnlock()
	return s.lastActive
}

func (s *localSession) Snapshot() *protocolpb.Snapshot {
	s.snapMu.RLock()
	defer s.snapMu.RUnlock()
	return s.snapshot
}

func (s *localSession) Size() (int, int) {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	return s.cols, s.rows
}

func (s *localSession) Resize(cols, rows int) (*protocolpb.Snapshot, error) {
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("invalid size")
	}
	s.emuMu.Lock()
	prevCols := s.cols
	prevRows := s.rows
	if !s.allowRemoteResize && (cols != prevCols || rows != prevRows) {
		s.armIgnoreNextPTYOutput()
	} else {
		s.clearIgnoredPTYOutput()
	}
	prevSnap := cloneSnapshot(s.Snapshot())
	prevPreserved, prevOriginCol, prevOriginRow := func() (*protocolpb.Snapshot, int, int) {
		s.snapMu.RLock()
		defer s.snapMu.RUnlock()
		return cloneSnapshot(s.preserved), s.preserveOriginCol, s.preserveOriginRow
	}()
	s.cols = cols
	s.rows = rows
	err := s.resizePTY(cols, rows)
	if s.emulator == nil {
		s.emuMu.Unlock()
		if s.logger != nil {
			s.logger.Debug("session.resize.skip.emulator.nil", "session", s.id, "cols", cols, "rows", rows)
		}
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("emulator not ready")
	}
	if err == nil {
		s.emulator.Resize(cols, rows)
	}
	rawSnap, snapErr := s.emulator.Snapshot()
	s.emuMu.Unlock()
	if err != nil {
		return nil, err
	}
	if snapErr != nil {
		return nil, snapErr
	}
	if s.allowRemoteResize {
		if s.onSnapshot != nil {
			s.onSnapshot(rawSnap)
		}
		snap := protocol.SnapshotToProto(rawSnap)
		s.snapMu.Lock()
		s.snapshot = snap
		s.preserved = cloneSnapshot(snap)
		s.preserveOriginCol = 0
		s.preserveOriginRow = 0
		s.snapMu.Unlock()
		return snap, nil
	}
	if prevPreserved == nil {
		if prevSnap != nil {
			prevPreserved = cloneSnapshot(prevSnap)
		} else {
			prevPreserved = cloneSnapshot(protocol.SnapshotToProto(rawSnap))
		}
	}
	if prevSnap == nil {
		prevSnap = cropSnapshotToViewport(prevPreserved, prevOriginCol, prevOriginRow, prevCols, prevRows)
	}
	relOriginCol, relOriginRow := viewportOriginForSnapshot(prevSnap, cols, rows)
	nextOriginCol, nextOriginRow := normalizeViewportOrigin(
		prevPreserved,
		prevOriginCol+relOriginCol,
		prevOriginRow+relOriginRow,
		cols,
		rows,
	)
	if (cols < prevCols || rows < prevRows) && prevSnap != nil {
		s.setPendingPreservedScrollbackFromSnapshot(prevSnap)
		s.appendScrollback(hiddenRowsFromViewport(prevSnap, relOriginRow, rows))
		s.snapMu.Lock()
		s.preserved = prevPreserved
		s.preserveOriginCol = nextOriginCol
		s.preserveOriginRow = nextOriginRow
		s.snapshot = cropSnapshotToViewport(prevPreserved, nextOriginCol, nextOriginRow, cols, rows)
		snap := s.snapshot
		s.snapMu.Unlock()
		return snap, nil
	}
	displayOriginCol, displayOriginRow := viewportOriginForSnapshot(prevPreserved, cols, rows)
	displayOriginCol, displayOriginRow = normalizeViewportOrigin(prevPreserved, displayOriginCol, displayOriginRow, cols, rows)
	s.snapMu.Lock()
	s.preserved = prevPreserved
	s.preserveOriginCol = prevOriginCol
	s.preserveOriginRow = prevOriginRow
	s.snapshot = cropSnapshotToViewport(prevPreserved, displayOriginCol, displayOriginRow, cols, rows)
	snap := s.snapshot
	s.snapMu.Unlock()
	if cols > prevCols || rows > prevRows {
		s.loadEmulatorSnapshot(snap)
	}
	return snap, nil
}

func (s *localSession) storeSnapshot(snap *protocolpb.Snapshot) *protocolpb.Snapshot {
	s.snapMu.Lock()
	s.preserved = mergePreservedSnapshotAt(
		s.preserved,
		snap,
		s.preserveOriginCol,
		s.preserveOriginRow,
		int(snap.GetCols()),
		int(snap.GetRows()),
	)
	displayOriginCol, displayOriginRow := viewportOriginForSnapshot(s.preserved, int(snap.GetCols()), int(snap.GetRows()))
	displayOriginCol, displayOriginRow = normalizeViewportOrigin(s.preserved, displayOriginCol, displayOriginRow, int(snap.GetCols()), int(snap.GetRows()))
	s.snapshot = cropSnapshotToViewport(
		s.preserved,
		displayOriginCol,
		displayOriginRow,
		int(snap.GetCols()),
		int(snap.GetRows()),
	)
	stored := s.snapshot
	s.snapMu.Unlock()
	return stored
}

func (s *localSession) RespawnEnabled() bool {
	s.respawnMu.RLock()
	defer s.respawnMu.RUnlock()
	return s.respawn
}

func (s *localSession) ToggleRespawn() bool {
	s.respawnMu.Lock()
	s.respawn = !s.respawn
	enabled := s.respawn
	s.respawnMu.Unlock()
	return enabled
}

func (s *localSession) Offline() bool {
	s.offlineMu.RLock()
	defer s.offlineMu.RUnlock()
	return s.offline
}

func (s *localSession) SetOffline(v bool) {
	s.offlineMu.Lock()
	s.offline = v
	s.offlineMu.Unlock()
	if s.publisher != nil {
		s.publisher.SetOffline(v)
	}
}

func (s *localSession) ToggleOffline() bool {
	s.offlineMu.Lock()
	s.offline = !s.offline
	offline := s.offline
	s.offlineMu.Unlock()
	if s.publisher != nil {
		s.publisher.SetOffline(offline)
	}
	return offline
}

func (s *localSession) Stop() {
	s.notifyRemoteSessionClosed("stopped")
	if s.cancel != nil {
		s.cancel()
	}
	s.closePTY()
}

func (s *localSession) notifyRemoteSessionClosed(reason string) {
	if s.publisher == nil || s.Offline() {
		return
	}
	s.closeOnce.Do(func() {
		s.publisher.SendSessionClosed(reason)
	})
}

func (s *localSession) writePTY(data []byte) (int, error) {
	return s.writePTYWithMode(data, false)
}

func (s *localSession) writeTerminalReply(data []byte) (int, error) {
	return s.writePTYWithMode(data, true)
}

func (s *localSession) writePTYWithMode(data []byte, disableEcho bool) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if len(data) > 0 {
		s.clearIgnoredPTYOutput()
	}
	s.ptyMu.RLock()
	ptyFile := s.pty
	ttyFile := s.tty
	s.ptyMu.RUnlock()
	if ptyFile == nil {
		return 0, errPTYNotReady
	}
	restoreEcho := func() {}
	if disableEcho && ttyFile != nil {
		if restore, err := disableTTYEcho(ttyFile); err == nil && restore != nil {
			restoreEcho = restore
		}
	}
	defer restoreEcho()
	sleep := time.Sleep
	if s.clock != nil {
		sleep = s.clock.Sleep
	}
	return writeAllWithRetry(s.ctx, sleep, data, ptyFile.Write)
}

func (s *localSession) sendRemoteEOF() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.ptyMu.RLock()
	ptyFile := s.pty
	ttyFile := s.tty
	s.ptyMu.RUnlock()
	if ptyFile == nil {
		return errPTYNotReady
	}

	veof := byte(0x04)
	s.veofMu.Lock()
	if s.veofSet && s.veofOrig != 0 {
		veof = s.veofOrig
	}
	s.veofMu.Unlock()
	if ttyFile != nil {
		_ = setVEOF(ttyFile, veof)
		defer func() {
			s.applyVEOF(s.holder())
		}()
	}

	sleep := time.Sleep
	if s.clock != nil {
		sleep = s.clock.Sleep
	}
	_, err := writeAllWithRetry(s.ctx, sleep, []byte{veof}, ptyFile.Write)
	return err
}

func writeAllWithRetry(ctx context.Context, sleep func(time.Duration), data []byte, write func([]byte) (int, error)) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	written := 0
	for written < len(data) {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return written, err
			}
		}
		n, err := write(data[written:])
		if n > 0 {
			written += n
		}
		if err == nil {
			if n == 0 {
				return written, io.ErrShortWrite
			}
			continue
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			if ctx != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return written, ctxErr
				}
			}
			if sleep != nil {
				sleep(2 * time.Millisecond)
			}
			continue
		}
		return written, err
	}
	return written, nil
}

func (s *localSession) resizePTY(cols, rows int) error {
	s.ptyMu.RLock()
	ptyFile := s.pty
	s.ptyMu.RUnlock()
	if ptyFile == nil {
		return errPTYNotReady
	}
	return pty.Resize(ptyFile, cols, rows)
}

func (s *localSession) takeControl() {
	if s.publisher == nil {
		return
	}
	s.publisher.TakeControl()
	s.setHolder(host.HostControlID)
}

func (s *localSession) holder() string {
	s.holderMu.Lock()
	defer s.holderMu.Unlock()
	return s.holderID
}

func (s *localSession) filterRemoteInput(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	s.ptyMu.RLock()
	ttyFile := s.tty
	s.ptyMu.RUnlock()
	if ttyFile == nil {
		return data
	}
	return filterRemoteInput(ttyFile, data)
}

func (s *localSession) setHolder(holderID string) {
	s.holderMu.Lock()
	s.holderID = holderID
	s.holderMu.Unlock()
	s.applyVEOF(holderID)
}

func (s *localSession) captureVEOF() {
	s.ptyMu.RLock()
	ttyFile := s.tty
	s.ptyMu.RUnlock()
	if ttyFile == nil {
		return
	}
	val, err := getVEOF(ttyFile)
	if err != nil {
		return
	}
	s.veofMu.Lock()
	s.veofOrig = val
	s.veofSet = true
	s.veofMu.Unlock()
}

func (s *localSession) applyVEOF(holderID string) {
	s.ptyMu.RLock()
	ttyFile := s.tty
	s.ptyMu.RUnlock()
	if ttyFile == nil {
		return
	}
	s.veofMu.Lock()
	if !s.veofSet {
		s.veofMu.Unlock()
		return
	}
	orig := s.veofOrig
	s.veofMu.Unlock()
	target := orig
	if holderID != "" && holderID != host.HostControlID {
		target = 0
	}
	_ = setVEOF(ttyFile, target)
}

func (s *localSession) enqueueRemoteInput(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	payload := append([]byte(nil), data...)
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.remoteInputCh <- payload:
		return nil
	}
}

func (s *localSession) runRemoteInput() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case data := <-s.remoteInputCh:
			s.processRemoteInput(data)
		}
	}
}

func (s *localSession) processRemoteInput(data []byte) {
	if len(data) == 0 {
		return
	}
	if !inputAllLines(data) {
		if _, err := s.writePTY(data); err != nil && s.logger != nil && !errors.Is(err, errPTYNotReady) && !errors.Is(err, context.Canceled) {
			s.logger.Debug("session.remote.input.write.failed", "err", err, "session", s.id)
		}
		return
	}
	for _, b := range data {
		s.resetOutputNotify()
		if _, err := s.writePTY([]byte{b}); err != nil {
			if s.logger != nil && !errors.Is(err, errPTYNotReady) && !errors.Is(err, context.Canceled) {
				s.logger.Debug("session.remote.input.write.failed", "err", err, "session", s.id)
			}
			return
		}
		s.waitForOutputWall(remoteLineOutputTimeout)
	}
}

func (s *localSession) closePTY() {
	s.ptyMu.Lock()
	ptyFile := s.pty
	ttyFile := s.tty
	cmd := s.cmd
	s.pty = nil
	s.tty = nil
	s.cmd = nil
	s.ptyMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if ptyFile != nil {
		_ = ptyFile.Close()
	}
	if ttyFile != nil {
		_ = ttyFile.Close()
	}
}

func (s *localSession) runOnce(ctx context.Context) error {
	ptyFile, ttyFile, cmd, err := startShell(s.shell, s.term)
	if err != nil {
		return err
	}
	s.ptyMu.Lock()
	s.pty = ptyFile
	s.tty = ttyFile
	s.cmd = cmd
	s.ptyMu.Unlock()

	defer s.closePTY()

	s.captureVEOF()

	if s.cols <= 0 {
		s.cols = 80
	}
	if s.rows <= 0 {
		s.rows = 24
	}

	s.emuMu.Lock()
	s.emulator = emu.New(s.cols, s.rows)
	if emuImpl, ok := s.emulator.(*emu.Emulator); ok {
		if s.trace != nil {
			emuImpl.SetCursorTrace(func(ev emu.CursorTraceEvent) {
				s.trace.Event("emu_cursor_zero", map[string]any{
					"component":    "host",
					"session_id":   s.id,
					"reason":       ev.Reason,
					"screen":       ev.Screen,
					"old_x":        ev.Old.X,
					"old_y":        ev.Old.Y,
					"new_x":        ev.New.X,
					"new_y":        ev.New.Y,
					"recent_input": trace.SummarizeBytes(ev.Recent, 120),
					"recent_len":   len(ev.Recent),
				})
			})
			emuImpl.SetEventTrace(func(ev emu.Event) {
				privateVal := ""
				if ev.Private != 0 {
					privateVal = string(ev.Private)
				}
				s.trace.Event("emu_event", map[string]any{
					"component":        "host",
					"session_id":       s.id,
					"name":             ev.Name,
					"final":            string(ev.Final),
					"private":          privateVal,
					"params":           ev.Params,
					"screen":           ev.Screen,
					"arg_row":          ev.ArgRow,
					"arg_code":         ev.ArgCode,
					"payload_len":      ev.PayloadLen,
					"old_x":            ev.Old.Cursor.X,
					"old_y":            ev.Old.Cursor.Y,
					"new_x":            ev.New.Cursor.X,
					"new_y":            ev.New.Cursor.Y,
					"old_saved_x":      ev.Old.SavedCursor.X,
					"old_saved_y":      ev.Old.SavedCursor.Y,
					"new_saved_x":      ev.New.SavedCursor.X,
					"new_saved_y":      ev.New.SavedCursor.Y,
					"old_scroll_top":   ev.Old.ScrollTop,
					"old_scroll_bot":   ev.Old.ScrollBottom,
					"new_scroll_top":   ev.New.ScrollTop,
					"new_scroll_bot":   ev.New.ScrollBottom,
					"old_origin_mode":  ev.Old.OriginMode,
					"new_origin_mode":  ev.New.OriginMode,
					"old_wrap_mode":    ev.Old.WrapMode,
					"new_wrap_mode":    ev.New.WrapMode,
					"old_insert_mode":  ev.Old.InsertMode,
					"new_insert_mode":  ev.New.InsertMode,
					"old_newline_mode": ev.Old.NewLineMode,
					"new_newline_mode": ev.New.NewLineMode,
					"old_cursor_vis":   ev.Old.CursorVisible,
					"new_cursor_vis":   ev.New.CursorVisible,
					"old_inline_row":   ev.Old.InlineOriginRow,
					"new_inline_row":   ev.New.InlineOriginRow,
					"old_inline_set":   ev.Old.InlineOriginSet,
					"new_inline_set":   ev.New.InlineOriginSet,
					"old_alt_screen":   ev.Old.AltScreen,
					"new_alt_screen":   ev.New.AltScreen,
					"cols":             ev.New.Cols,
					"rows":             ev.New.Rows,
				})
			})
		}
		emuImpl.SetScrollbackLimit(s.scrollbackLines)
	}
	s.emuMu.Unlock()
	if s.logger != nil {
		s.logger.Trace("session.emulator.ready", "session", s.id, "cols", s.cols, "rows", s.rows)
	}
	_ = s.resizePTY(s.cols, s.rows)
	s.emuMu.Lock()
	rawSnap, snapErr := s.emulator.Snapshot()
	s.emuMu.Unlock()
	if snapErr == nil {
		s.storeSnapshot(protocol.SnapshotToProto(rawSnap))
	}
	_ = setNonblock(ptyFile, true)
	defer func() {
		_ = setNonblock(ptyFile, false)
	}()

	localErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := readPTY(ctx, ptyFile, buf)
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
					s.clock.Sleep(10 * time.Millisecond)
					continue
				}
				if !errors.Is(err, io.EOF) {
					s.logger.Debug("session.pty.read.failed", "err", err, "session", s.id)
				}
				select {
				case localErr <- err:
				default:
				}
				return
			}
			data := buf[:n]
			if s.onPTYRead != nil {
				cp := make([]byte, len(data))
				copy(cp, data)
				s.onPTYRead(cp)
			}
			filtered := s.filterOSCOutput(data)
			suppressResizeRedraw := s.shouldIgnoreNextPTYOutput(filtered)
			var scrollRows []terminal.ScrollbackRow
			s.emuMu.Lock()
			if err := s.emulator.Write(filtered); err != nil {
				s.logger.Debug("session.emulator.write.failed", "err", err, "session", s.id)
			}
			rawSnap, err := s.emulator.Snapshot()
			if scrollback, ok := s.emulator.(*emu.Emulator); ok {
				scrollRows = scrollback.DrainScrollback()
			}
			s.emuMu.Unlock()
			s.respondToTerminalQueries(data, rawSnap)
			if err != nil {
				select {
				case localErr <- err:
				default:
				}
				return
			}
			if suppressResizeRedraw {
				continue
			}
			if s.onSnapshot != nil {
				s.onSnapshot(rawSnap)
			}
			snap := s.storeSnapshot(protocol.SnapshotToProto(rawSnap))
			scrollRows = s.appendScrollback(s.drainScrollbackRows(scrollRows))
			if s.onOutput != nil {
				s.onOutput(s.id, filtered, snap)
			}
			s.notifyOutput()
			if s.publisher != nil {
				if len(scrollRows) > 0 {
					s.publisher.PublishScrollback(scrollRows, int(snap.Cols), false)
				}
				s.publisher.Publish(filtered, snap)
			}
		}
	}()

	if s.publisher != nil {
		s.publisher.TakeControl()
		if snap := s.Snapshot(); snap != nil {
			s.publisher.Publish(nil, snap)
		}
	}

	select {
	case <-ctx.Done():
	case <-waitProcess(cmd):
	case <-localErr:
	}

	return nil
}

func mergePreservedSnapshotAt(prev, current *protocolpb.Snapshot, originCol, originRow, overlayCols, overlayRows int) *protocolpb.Snapshot {
	if current == nil {
		return cloneSnapshot(prev)
	}
	if prev == nil {
		return cloneSnapshot(current)
	}
	prevAlt := prev.GetMode()&terminal.SnapshotModeAltScreen != 0
	currAlt := current.GetMode()&terminal.SnapshotModeAltScreen != 0
	if prevAlt != currAlt {
		return cloneSnapshot(current)
	}

	prevCols := int(prev.GetCols())
	prevRows := int(prev.GetRows())
	currCols := int(current.GetCols())
	currRows := int(current.GetRows())
	if originCol < 0 {
		originCol = 0
	}
	if originRow < 0 {
		originRow = 0
	}
	outCols := maxInt(prevCols, originCol+currCols)
	outRows := maxInt(prevRows, originRow+currRows)
	out := blankSnapshot(outCols, outRows)
	out.Mode = current.GetMode()
	out.Title = current.GetTitle()
	out.CursorVisible = current.GetCursorVisible()
	if current.Cursor != nil {
		cursorX := originCol + int(current.Cursor.GetX())
		cursorY := originRow + int(current.Cursor.GetY())
		if cursorX < 0 {
			cursorX = 0
		}
		if cursorY < 0 {
			cursorY = 0
		}
		if cursorX >= outCols {
			cursorX = outCols - 1
		}
		if cursorY >= outRows {
			cursorY = outRows - 1
		}
		out.Cursor = &protocolpb.Cursor{X: uint32(cursorX), Y: uint32(cursorY)}
	}

	copySnapshotRegion(out, prev, int(prev.GetCols()), int(prev.GetRows()))
	copySnapshotRegionAtOffset(out, current, originCol, originRow, overlayCols, overlayRows)
	return out
}

func hiddenRowsFromViewport(snap *protocolpb.Snapshot, originRow, visibleRows int) []terminal.ScrollbackRow {
	rows := snapshotToScrollbackRows(snap)
	if len(rows) == 0 {
		return nil
	}
	if originRow < 0 {
		originRow = 0
	}
	if visibleRows < 0 {
		visibleRows = 0
	}
	if originRow >= len(rows) {
		return cloneScrollbackRows(rows)
	}
	end := originRow + visibleRows
	if end > len(rows) {
		end = len(rows)
	}
	if originRow == 0 && end >= len(rows) {
		return nil
	}
	out := make([]terminal.ScrollbackRow, 0, originRow+len(rows)-end)
	out = append(out, cloneScrollbackRows(rows[:originRow])...)
	out = append(out, cloneScrollbackRows(rows[end:])...)
	return out
}

func cloneSnapshot(snap *protocolpb.Snapshot) *protocolpb.Snapshot {
	if snap == nil {
		return nil
	}
	out := &protocolpb.Snapshot{
		Cols:          snap.GetCols(),
		Rows:          snap.GetRows(),
		Runes:         append([]uint32(nil), snap.GetRunes()...),
		Modes:         append([]int32(nil), snap.GetModes()...),
		Fg:            append([]uint32(nil), snap.GetFg()...),
		Bg:            append([]uint32(nil), snap.GetBg()...),
		CursorVisible: snap.GetCursorVisible(),
		Mode:          snap.GetMode(),
		Title:         snap.GetTitle(),
		Graphemes:     append([]string(nil), snap.GetGraphemes()...),
	}
	if snap.Cursor != nil {
		out.Cursor = &protocolpb.Cursor{X: snap.Cursor.GetX(), Y: snap.Cursor.GetY()}
	}
	return out
}

func cropSnapshotToViewport(snap *protocolpb.Snapshot, originCol, originRow, cols, rows int) *protocolpb.Snapshot {
	if snap == nil {
		return nil
	}
	if cols <= 0 {
		cols = int(snap.GetCols())
	}
	if rows <= 0 {
		rows = int(snap.GetRows())
	}
	if originCol < 0 {
		originCol = 0
	}
	if originRow < 0 {
		originRow = 0
	}
	out := blankSnapshot(cols, rows)
	out.Mode = snap.GetMode()
	out.Title = snap.GetTitle()
	out.CursorVisible = snap.GetCursorVisible()
	if snap.Cursor != nil {
		cursorX := int(snap.Cursor.GetX()) - originCol
		cursorY := int(snap.Cursor.GetY()) - originRow
		if cols > 0 && cursorX >= cols {
			cursorX = cols - 1
		}
		if rows > 0 && cursorY >= rows {
			cursorY = rows - 1
		}
		if cursorX < 0 {
			cursorX = 0
		}
		if cursorY < 0 {
			cursorY = 0
		}
		out.Cursor = &protocolpb.Cursor{X: uint32(cursorX), Y: uint32(cursorY)}
	}
	copySnapshotRegionFromOffset(out, snap, originCol, originRow, cols, rows)
	return out
}

func viewportOriginForSnapshot(snap *protocolpb.Snapshot, viewCols, viewRows int) (int, int) {
	if snap == nil {
		return 0, 0
	}
	cols := int(snap.GetCols())
	rows := int(snap.GetRows())
	cursorX := 0
	cursorY := 0
	if snap.Cursor != nil {
		cursorX = int(snap.Cursor.GetX())
		cursorY = int(snap.Cursor.GetY())
	}
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
	return render.ViewportOriginForCursor(cols, rows, viewCols, viewRows, cursorX, cursorY)
}

func normalizeViewportOrigin(snap *protocolpb.Snapshot, originCol, originRow, viewCols, viewRows int) (int, int) {
	if snap == nil {
		if originCol < 0 {
			originCol = 0
		}
		if originRow < 0 {
			originRow = 0
		}
		return originCol, originRow
	}
	cols := int(snap.GetCols())
	rows := int(snap.GetRows())
	maxCol := cols - viewCols
	maxRow := rows - viewRows
	if maxCol < 0 {
		maxCol = 0
	}
	if maxRow < 0 {
		maxRow = 0
	}
	if originCol < 0 {
		originCol = 0
	}
	if originRow < 0 {
		originRow = 0
	}
	if originCol > maxCol {
		originCol = maxCol
	}
	if originRow > maxRow {
		originRow = maxRow
	}
	return originCol, originRow
}

func blankSnapshot(cols, rows int) *protocolpb.Snapshot {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	size := cols * rows
	return &protocolpb.Snapshot{
		Cols:  uint32(cols),
		Rows:  uint32(rows),
		Runes: make([]uint32, size),
		Modes: make([]int32, size),
		Fg:    make([]uint32, size),
		Bg:    make([]uint32, size),
	}
}

func copySnapshotRegion(dst, src *protocolpb.Snapshot, limitCols, limitRows int) {
	copySnapshotRegionAtOffset(dst, src, 0, 0, limitCols, limitRows)
}

func copySnapshotRegionFromOffset(dst, src *protocolpb.Snapshot, originCol, originRow, limitCols, limitRows int) {
	if dst == nil || src == nil {
		return
	}
	if originCol < 0 {
		originCol = 0
	}
	if originRow < 0 {
		originRow = 0
	}
	dstCols := int(dst.GetCols())
	dstRows := int(dst.GetRows())
	srcCols := int(src.GetCols())
	srcRows := int(src.GetRows())
	rows := minInt(dstRows, srcRows-originRow)
	cols := minInt(dstCols, srcCols-originCol)
	if limitRows > 0 && rows > limitRows {
		rows = limitRows
	}
	if limitCols > 0 && cols > limitCols {
		cols = limitCols
	}
	if rows <= 0 || cols <= 0 {
		return
	}
	if len(src.GetGraphemes()) > 0 && len(dst.Graphemes) == 0 {
		dst.Graphemes = make([]string, dstCols*dstRows)
	}
	for y := 0; y < rows; y++ {
		srcRow := (originRow + y) * srcCols
		dstRow := y * dstCols
		for x := 0; x < cols; x++ {
			srcIdx := srcRow + originCol + x
			dstIdx := dstRow + x
			if srcIdx < len(src.Runes) {
				dst.Runes[dstIdx] = src.Runes[srcIdx]
			}
			if srcIdx < len(src.Modes) {
				dst.Modes[dstIdx] = src.Modes[srcIdx]
			}
			if srcIdx < len(src.Fg) {
				dst.Fg[dstIdx] = src.Fg[srcIdx]
			}
			if srcIdx < len(src.Bg) {
				dst.Bg[dstIdx] = src.Bg[srcIdx]
			}
			if len(dst.Graphemes) > 0 {
				if srcIdx < len(src.Graphemes) {
					dst.Graphemes[dstIdx] = src.Graphemes[srcIdx]
				} else {
					dst.Graphemes[dstIdx] = ""
				}
			}
		}
	}
}

func copySnapshotRegionAtOffset(dst, src *protocolpb.Snapshot, originCol, originRow, limitCols, limitRows int) {
	if dst == nil || src == nil {
		return
	}
	if originCol < 0 {
		originCol = 0
	}
	if originRow < 0 {
		originRow = 0
	}
	dstCols := int(dst.GetCols())
	dstRows := int(dst.GetRows())
	srcCols := int(src.GetCols())
	srcRows := int(src.GetRows())
	rows := minInt(dstRows-originRow, srcRows)
	cols := minInt(dstCols-originCol, srcCols)
	if limitRows > 0 && rows > limitRows {
		rows = limitRows
	}
	if limitCols > 0 && cols > limitCols {
		cols = limitCols
	}
	if rows <= 0 || cols <= 0 {
		return
	}
	if len(src.GetGraphemes()) > 0 && len(dst.Graphemes) == 0 {
		dst.Graphemes = make([]string, dstCols*dstRows)
	}
	for y := 0; y < rows; y++ {
		srcRow := y * srcCols
		dstRow := (originRow + y) * dstCols
		for x := 0; x < cols; x++ {
			srcIdx := srcRow + x
			dstIdx := dstRow + originCol + x
			if srcIdx < len(src.Runes) {
				dst.Runes[dstIdx] = src.Runes[srcIdx]
			}
			if srcIdx < len(src.Modes) {
				dst.Modes[dstIdx] = src.Modes[srcIdx]
			}
			if srcIdx < len(src.Fg) {
				dst.Fg[dstIdx] = src.Fg[srcIdx]
			}
			if srcIdx < len(src.Bg) {
				dst.Bg[dstIdx] = src.Bg[srcIdx]
			}
			if len(dst.Graphemes) > 0 {
				if srcIdx < len(src.Graphemes) {
					dst.Graphemes[dstIdx] = src.Graphemes[srcIdx]
				} else {
					dst.Graphemes[dstIdx] = ""
				}
			}
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *localSession) respondToTerminalQueries(data []byte, snap terminal.Snapshot) {
	s.recordRecentInput(data)
	for _, b := range data {
		final, params, private, ok := s.csiState.Feed(b)
		if !ok {
			continue
		}
		switch final {
		case 'n':
			if len(params) != 1 {
				continue
			}
			switch private {
			case 0:
				switch params[0] {
				case 5:
					if s.trace != nil {
						s.trace.Event("cursor_status_request", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "",
							"param":      5,
						})
					}
					resp := []byte("\x1b[0n")
					if s.trace != nil {
						s.trace.Event("cursor_status_response", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "",
							"param":      5,
							"response":   trace.SummarizeBytes(resp, 80),
						})
					}
					_, _ = s.writeTerminalReply(resp)
				case 6:
					if s.trace != nil {
						recent := s.recentInputSnapshot()
						signals := analyzeRecentInput(recent)
						altActive, altKnown := s.emuAltScreenActive()
						s.trace.Event("cursor_position_request", map[string]any{
							"component":                "host",
							"session_id":               s.id,
							"private":                  "",
							"param":                    6,
							"recent_input":             trace.SummarizeBytes(recent, 120),
							"recent_input_len":         len(recent),
							"emu_alt_screen":           altActive,
							"emu_alt_screen_known":     altKnown,
							"recent_has_dsr":           signals.hasDSR,
							"recent_has_home":          signals.hasHome,
							"recent_has_clear":         signals.hasClear,
							"recent_has_alt_enter":     signals.hasAltEnter,
							"recent_has_alt_leave":     signals.hasAltLeave,
							"recent_has_cup":           signals.hasCUP,
							"recent_has_cup_home":      signals.hasCUPHome,
							"recent_has_ed":            signals.hasED,
							"recent_has_ed2":           signals.hasED2,
							"recent_has_scroll_region": signals.hasScrollRegion,
							"recent_has_ris":           signals.hasRIS,
						})
					}
					row, col, ok := s.cursorQueryPosition(snap)
					if ok {
						if origin := inlineOriginRow(snap.Cursor.Y, row, snap.Rows); origin > 0 {
							s.setInlineOriginRow(origin)
						}
					} else {
						row = snap.Cursor.Y + 1
						col = snap.Cursor.X + 1
					}
					if row < 1 {
						row = 1
					}
					if col < 1 {
						col = 1
					}
					resp := []byte(fmt.Sprintf("\x1b[%d;%dR", row, col))
					if s.trace != nil {
						s.trace.Event("cursor_position_response", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "",
							"param":      6,
							"row":        row,
							"col":        col,
							"ok":         ok,
							"cursor_x":   snap.Cursor.X,
							"cursor_y":   snap.Cursor.Y,
							"cols":       snap.Cols,
							"rows":       snap.Rows,
							"response":   trace.SummarizeBytes(resp, 80),
						})
					}
					_, _ = s.writeTerminalReply(resp)
				}
			case '?':
				switch params[0] {
				case 5:
					if s.trace != nil {
						s.trace.Event("cursor_status_request", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "?",
							"param":      5,
						})
					}
					resp := []byte("\x1b[?0n")
					if s.trace != nil {
						s.trace.Event("cursor_status_response", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "?",
							"param":      5,
							"response":   trace.SummarizeBytes(resp, 80),
						})
					}
					_, _ = s.writeTerminalReply(resp)
				case 6:
					if s.trace != nil {
						recent := s.recentInputSnapshot()
						signals := analyzeRecentInput(recent)
						altActive, altKnown := s.emuAltScreenActive()
						s.trace.Event("cursor_position_request", map[string]any{
							"component":                "host",
							"session_id":               s.id,
							"private":                  "?",
							"param":                    6,
							"recent_input":             trace.SummarizeBytes(recent, 120),
							"recent_input_len":         len(recent),
							"emu_alt_screen":           altActive,
							"emu_alt_screen_known":     altKnown,
							"recent_has_dsr":           signals.hasDSR,
							"recent_has_home":          signals.hasHome,
							"recent_has_clear":         signals.hasClear,
							"recent_has_alt_enter":     signals.hasAltEnter,
							"recent_has_alt_leave":     signals.hasAltLeave,
							"recent_has_cup":           signals.hasCUP,
							"recent_has_cup_home":      signals.hasCUPHome,
							"recent_has_ed":            signals.hasED,
							"recent_has_ed2":           signals.hasED2,
							"recent_has_scroll_region": signals.hasScrollRegion,
							"recent_has_ris":           signals.hasRIS,
						})
					}
					row, col, ok := s.cursorQueryPosition(snap)
					if ok {
						if origin := inlineOriginRow(snap.Cursor.Y, row, snap.Rows); origin > 0 {
							s.setInlineOriginRow(origin)
						}
					} else {
						row = snap.Cursor.Y + 1
						col = snap.Cursor.X + 1
					}
					if row < 1 {
						row = 1
					}
					if col < 1 {
						col = 1
					}
					resp := []byte(fmt.Sprintf("\x1b[?%d;%dR", row, col))
					if s.trace != nil {
						s.trace.Event("cursor_position_response", map[string]any{
							"component":  "host",
							"session_id": s.id,
							"private":    "?",
							"param":      6,
							"row":        row,
							"col":        col,
							"ok":         ok,
							"cursor_x":   snap.Cursor.X,
							"cursor_y":   snap.Cursor.Y,
							"cols":       snap.Cols,
							"rows":       snap.Rows,
							"response":   trace.SummarizeBytes(resp, 80),
						})
					}
					_, _ = s.writeTerminalReply(resp)
				}
			}
		case 'u':
			if private != '?' {
				continue
			}
			if s.trace != nil {
				s.trace.Event("keyboard_enhancement_request", map[string]any{
					"component":  "host",
					"session_id": s.id,
					"private":    "?",
					"param":      params,
				})
			}
			resp := []byte("\x1b[?0u")
			if s.trace != nil {
				s.trace.Event("keyboard_enhancement_response", map[string]any{
					"component":  "host",
					"session_id": s.id,
					"private":    "?",
					"response":   trace.SummarizeBytes(resp, 80),
				})
			}
			_, _ = s.writeTerminalReply(resp)
		case 'c':
			switch private {
			case 0:
				if s.trace != nil {
					s.trace.Event("device_attributes_request", map[string]any{
						"component":  "host",
						"session_id": s.id,
						"private":    "",
					})
				}
				resp := []byte("\x1b[?1;2c")
				if s.trace != nil {
					s.trace.Event("device_attributes_response", map[string]any{
						"component":  "host",
						"session_id": s.id,
						"private":    "",
						"response":   trace.SummarizeBytes(resp, 80),
					})
				}
				_, _ = s.writeTerminalReply(resp)
			case '>':
				if s.trace != nil {
					s.trace.Event("device_attributes_request", map[string]any{
						"component":  "host",
						"session_id": s.id,
						"private":    ">",
					})
				}
				resp := []byte("\x1b[>0;0;0c")
				if s.trace != nil {
					s.trace.Event("device_attributes_response", map[string]any{
						"component":  "host",
						"session_id": s.id,
						"private":    ">",
						"response":   trace.SummarizeBytes(resp, 80),
					})
				}
				_, _ = s.writeTerminalReply(resp)
			}
		}
	}
}

func (s *localSession) filterOSCOutput(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	for _, b := range data {
		code, payload, raw, ok := s.oscState.Feed(b)
		if !ok {
			continue
		}
		if !s.shouldSuppressOSC(code) {
			s.oscState.AddPassthrough(raw)
			continue
		}
		if payload == "?" {
			s.respondToOSCQuery(code)
		}
	}
	return s.filterOSCEcho(s.oscState.DrainPassthrough())
}

func (s *localSession) shouldSuppressOSC(code int) bool {
	switch code {
	case 10, 11, 12:
		return true
	default:
		return false
	}
}

func (s *localSession) respondToOSCQuery(code int) {
	color := s.oscQueryColor(code)
	if color == "" {
		return
	}
	resp := []byte(fmt.Sprintf("\x1b]%d;%s\x07", code, color))
	if s.trace != nil {
		s.trace.Event("osc_query_request", map[string]any{
			"component":  "host",
			"session_id": s.id,
			"code":       code,
		})
	}
	if s.trace != nil {
		s.trace.Event("osc_query_response", map[string]any{
			"component":  "host",
			"session_id": s.id,
			"code":       code,
			"response":   trace.SummarizeBytes(resp, 120),
		})
	}
	_, _ = s.writeTerminalReply(resp)
}

func (s *localSession) filterOSCEcho(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	for _, b := range data {
		s.oscEchoState.Feed(b)
	}
	return s.oscEchoState.DrainPassthrough()
}

func (s *localSession) setOscDefaults(fg, bg, cursor string) {
	s.oscDefaultsMu.Lock()
	if fg != "" {
		s.oscDefaultFg = fg
	}
	if bg != "" {
		s.oscDefaultBg = bg
	}
	if cursor != "" {
		s.oscDefaultCursor = cursor
	}
	s.oscDefaultsMu.Unlock()
}

func (s *localSession) oscQueryColor(code int) string {
	s.oscDefaultsMu.RLock()
	fg := s.oscDefaultFg
	bg := s.oscDefaultBg
	cursor := s.oscDefaultCursor
	s.oscDefaultsMu.RUnlock()
	switch code {
	case 10:
		if fg != "" {
			return fg
		}
		return "rgb:ffff/ffff/ffff"
	case 11:
		if bg != "" {
			return bg
		}
		return "rgb:0000/0000/0000"
	case 12:
		if cursor != "" {
			return cursor
		}
		if fg != "" {
			return fg
		}
		return "rgb:ffff/ffff/ffff"
	default:
		return ""
	}
}

func (s *localSession) cursorQueryPosition(snap terminal.Snapshot) (row, col int, ok bool) {
	if s.cursorQuery == nil {
		return 0, 0, false
	}
	return s.cursorQuery(snap)
}

type csiParser struct {
	state      int
	private    byte
	params     []int
	current    int
	hasCurrent bool
}

func (p *csiParser) reset() {
	p.state = 0
	p.private = 0
	p.params = p.params[:0]
	p.current = 0
	p.hasCurrent = false
}

func (p *csiParser) Feed(b byte) (final byte, params []int, private byte, ok bool) {
	switch p.state {
	case 0:
		if b == 0x1b {
			p.state = 1
		}
	case 1:
		if b == '[' {
			p.state = 2
			p.private = 0
			p.params = p.params[:0]
			p.current = 0
			p.hasCurrent = false
		} else if b == 0x1b {
			p.state = 1
		} else {
			p.state = 0
		}
	case 2:
		switch {
		case b >= '0' && b <= '9':
			p.current = p.current*10 + int(b-'0')
			p.hasCurrent = true
		case b == ';':
			if p.hasCurrent {
				p.params = append(p.params, p.current)
				p.current = 0
				p.hasCurrent = false
			} else {
				p.params = append(p.params, -1)
			}
		case b == '?' || b == '>':
			if p.private == 0 && !p.hasCurrent && len(p.params) == 0 {
				p.private = b
			} else {
				p.reset()
			}
		case b >= 0x40 && b <= 0x7e:
			if p.hasCurrent {
				p.params = append(p.params, p.current)
			}
			final = b
			params = append([]int(nil), p.params...)
			private = p.private
			p.reset()
			return final, params, private, true
		default:
			p.reset()
		}
	default:
		p.reset()
	}
	return 0, nil, 0, false
}

func parseOSCQuery(buf []byte) (int, string, bool) {
	if len(buf) == 0 {
		return 0, "", false
	}
	code := 0
	i := 0
	for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
		code = code*10 + int(buf[i]-'0')
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	if i < len(buf) && buf[i] == ';' {
		return code, string(buf[i+1:]), true
	}
	return code, "", true
}

type oscEchoParser struct {
	state       int
	raw         []byte
	code        int
	digits      int
	passthrough []byte
}

func (p *oscEchoParser) reset() {
	p.state = 0
	p.raw = p.raw[:0]
	p.code = 0
	p.digits = 0
}

func (p *oscEchoParser) flushRaw() {
	if len(p.raw) > 0 {
		p.passthrough = append(p.passthrough, p.raw...)
	}
	p.reset()
}

func (p *oscEchoParser) Feed(b byte) {
	switch p.state {
	case 0:
		if b == '^' {
			p.state = 1
			p.raw = p.raw[:0]
			p.raw = append(p.raw, b)
			return
		}
		p.passthrough = append(p.passthrough, b)
	case 1:
		p.raw = append(p.raw, b)
		if b == '[' {
			p.state = 2
			return
		}
		p.flushRaw()
	case 2:
		p.raw = append(p.raw, b)
		if b == ']' {
			p.state = 3
			p.code = 0
			p.digits = 0
			return
		}
		p.flushRaw()
	case 3:
		p.raw = append(p.raw, b)
		switch {
		case b >= '0' && b <= '9':
			p.code = p.code*10 + int(b-'0')
			p.digits++
		case b == ';' && p.digits > 0 && (p.code == 10 || p.code == 11 || p.code == 12):
			p.state = 4
		default:
			p.flushRaw()
		}
	case 4:
		p.raw = append(p.raw, b)
		if b == '^' {
			p.state = 5
		}
	case 5:
		p.raw = append(p.raw, b)
		if b == 'G' {
			p.reset()
			return
		}
		p.state = 4
	default:
		p.flushRaw()
	}
}

func (p *oscEchoParser) DrainPassthrough() []byte {
	if len(p.passthrough) == 0 {
		return nil
	}
	out := append([]byte(nil), p.passthrough...)
	p.passthrough = p.passthrough[:0]
	return out
}

func (s *localSession) Run() {
	for {
		err := s.runOnce(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		if !s.RespawnEnabled() {
			s.notifyRemoteSessionClosed("terminated")
			if s.onExit != nil {
				s.onExit(s.id, err)
			}
			if s.cancel != nil {
				s.cancel()
			}
			return
		}
		s.clock.Sleep(100 * time.Millisecond)
	}
}
