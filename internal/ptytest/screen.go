package ptytest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/mattn/go-runewidth"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/sys/unix"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

// Screen represents a rendered snapshot of the PTY viewport.
type Screen struct {
	Cols  int
	Rows  int
	Lines []string
}

// Cursor captures the 1-based cursor position from the emulator.
type Cursor struct {
	Row     int
	Col     int
	Visible bool
}

func (s Screen) String() string {
	return strings.Join(s.Lines, "\n")
}

// Row returns the line at index i (0-based), or empty if out of range.
func (s Screen) Row(i int) string {
	if i < 0 || i >= len(s.Lines) {
		return ""
	}
	return s.Lines[i]
}

// Diff returns a unified diff against the expected screen content.
func (s Screen) Diff(expected string) (string, bool) {
	got := s.String()
	if got == expected {
		return "", true
	}
	expLines := strings.Split(expected, "\n")
	gotLines := strings.Split(got, "\n")
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        expLines,
		B:        gotLines,
		FromFile: "expected",
		ToFile:   "got",
		Context:  3,
	})
	return diff, false
}

// Contains reports whether the screen contains substr.
func (s Screen) Contains(substr string) bool {
	return strings.Contains(s.String(), substr)
}

// Match reports whether the screen matches the regexp.
func (s Screen) Match(re *regexp.Regexp) bool {
	return re.MatchString(s.String())
}

// PTYSession wraps a PTY master/slave pair and emulator for tests.
type PTYSession struct {
	t *testing.T

	master *os.File
	slave  *os.File
	emu    terminal.Emulator
	size   *sizeProvider

	ctx     context.Context
	cancel  context.CancelFunc
	runErr  chan error
	cleanup func()
	closeMu sync.Once

	mu     sync.Mutex
	rawBuf bytes.Buffer

	errMu   sync.Mutex
	errSet  bool
	lastErr error

	readErrMu   sync.Mutex
	readErrSet  bool
	lastReadErr error

	clock clock.Clock
}

func newPTYSession(t *testing.T, master, slave *os.File, emu terminal.Emulator) *PTYSession {
	ctx, cancel := context.WithCancel(context.Background())
	s := &PTYSession{
		t:      t,
		master: master,
		slave:  slave,
		emu:    emu,
		ctx:    ctx,
		cancel: cancel,
		runErr: make(chan error, 1),
	}
	go s.readLoop()
	t.Cleanup(s.Close)
	return s
}

// NewPTYSession constructs a PTY session for custom test runners.
func NewPTYSession(t *testing.T, master, slave *os.File, cols, rows int) *PTYSession {
	t.Helper()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	emu := emu.New(cols, rows)
	return newPTYSession(t, master, slave, emu)
}

// NewPTYSessionWithClock constructs a PTY session with an explicit clock.
func NewPTYSessionWithClock(t *testing.T, master, slave *os.File, cols, rows int, clk clock.Clock) *PTYSession {
	t.Helper()
	sess := NewPTYSession(t, master, slave, cols, rows)
	sess.clock = clk
	return sess
}

// OpenPTY opens a PTY pair for tests and resizes it to the requested size.
func OpenPTY(t *testing.T, cols, rows int) (*os.File, *os.File) {
	t.Helper()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}
	return master, slave
}

func (s *PTYSession) readLoop() {
	buf := make([]byte, 4096)
	pfd := []unix.PollFd{{Fd: int32(s.master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		ready, pollErr := unix.Poll(pfd, 1)
		if pollErr != nil {
			if errors.Is(pollErr, syscall.EINTR) {
				continue
			}
			s.readErrMu.Lock()
			if !s.readErrSet {
				s.readErrSet = true
				s.lastReadErr = pollErr
			}
			s.readErrMu.Unlock()
			return
		}
		if ready == 0 {
			continue
		}
		n, err := s.master.Read(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.rawBuf.Write(buf[:n])
			_ = s.emu.Write(buf[:n])
			s.mu.Unlock()
		}
		if err != nil {
			s.readErrMu.Lock()
			if !s.readErrSet {
				s.readErrSet = true
				s.lastReadErr = err
			}
			s.readErrMu.Unlock()
			return
		}
	}
}

// Close stops the PTY session and reports any run errors.
func (s *PTYSession) Close() {
	s.shutdown()
	s.errMu.Lock()
	if s.errSet {
		s.errMu.Unlock()
		return
	}
	s.errMu.Unlock()
	select {
	case err := <-s.runErr:
		s.errMu.Lock()
		s.lastErr = err
		s.errSet = true
		s.errMu.Unlock()
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil && s.t != nil {
			s.t.Fatalf("session error: %v", err)
		}
	default:
	}
}

// Cancel requests the session to stop without asserting on errors.
func (s *PTYSession) Cancel() {
	s.shutdown()
}

func (s *PTYSession) shutdown() {
	s.closeMu.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

// Send writes the provided string to the PTY master.
func (s *PTYSession) Send(data string) {
	s.t.Helper()
	_, _ = s.master.Write([]byte(data))
}

// SendBytes writes raw bytes to the PTY master.
func (s *PTYSession) SendBytes(data []byte) {
	s.t.Helper()
	_, _ = s.master.Write(data)
}

// SendCtrlL sends Ctrl+L to the PTY master.
func (s *PTYSession) SendCtrlL() {
	s.SendBytes([]byte{0x0c})
}

// Resize updates the PTY window and emulator size.
func (s *PTYSession) Resize(cols, rows int) {
	s.t.Helper()
	if s.slave != nil {
		_ = pty.Setsize(s.slave, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
	s.mu.Lock()
	if s.emu != nil {
		s.emu.Resize(cols, rows)
	}
	s.mu.Unlock()
	if s.size != nil {
		s.size.Set(cols, rows)
	}
}

// Wait sleeps for the provided duration.
func (s *PTYSession) Wait(d time.Duration) {
	Advance(s.Clock(), d)
}

// Screen returns the current rendered viewport.
func (s *PTYSession) Screen() Screen {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.emu.Snapshot()
	if err != nil {
		s.t.Fatalf("snapshot: %v", err)
	}
	return screenFromSnapshot(snap)
}

// Snapshot returns the raw terminal snapshot.
func (s *PTYSession) Snapshot() terminal.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.emu.Snapshot()
	if err != nil {
		s.t.Fatalf("snapshot: %v", err)
	}
	return snap
}

// DrainRaw returns and clears the raw PTY output buffer.
func (s *PTYSession) DrainRaw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.rawBuf.String()
	s.rawBuf.Reset()
	return out
}

// Clock returns the session clock or a real clock if unset.
func (s *PTYSession) Clock() clock.Clock {
	if s.clock == nil {
		return clock.New()
	}
	return s.clock
}

// Cursor returns the current cursor position.
func (s *PTYSession) Cursor() Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.emu.Snapshot()
	if err != nil {
		s.t.Fatalf("snapshot: %v", err)
	}
	row := snap.Cursor.Y + 1
	col := snap.Cursor.X + 1
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	return Cursor{
		Row:     row,
		Col:     col,
		Visible: snap.CursorVisible,
	}
}

// WaitErr waits for the session to exit and returns the error.
func (s *PTYSession) WaitErr(timeout time.Duration) (bool, error) {
	clk := s.Clock()
	if clk == nil {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case err := <-s.runErr:
			s.errMu.Lock()
			s.lastErr = err
			s.errSet = true
			s.errMu.Unlock()
			return true, err
		case <-timer.C:
			return false, nil
		}
	}
	if _, ok := clk.(advancingClock); ok {
		deadline := Now(clk).Add(timeout)
		for Now(clk).Before(deadline) {
			select {
			case err := <-s.runErr:
				s.errMu.Lock()
				s.lastErr = err
				s.errSet = true
				s.errMu.Unlock()
				return true, err
			default:
			}
			Advance(clk, 10*time.Millisecond)
		}
		select {
		case err := <-s.runErr:
			s.errMu.Lock()
			s.lastErr = err
			s.errSet = true
			s.errMu.Unlock()
			return true, err
		default:
			return false, nil
		}
	}
	timer := clk.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-s.runErr:
		s.errMu.Lock()
		s.lastErr = err
		s.errSet = true
		s.errMu.Unlock()
		return true, err
	case <-timer.C:
		return false, nil
	}
}

// ReadErr reports the first PTY read error, if any.
func (s *PTYSession) ReadErr() error {
	s.readErrMu.Lock()
	defer s.readErrMu.Unlock()
	return s.lastReadErr
}

// Context returns the session context.
func (s *PTYSession) Context() context.Context {
	return s.ctx
}

// SetRunErr publishes a run error to the session channel.
func (s *PTYSession) SetRunErr(err error) {
	select {
	case s.runErr <- err:
	default:
	}
}

// CellAt returns the cell at row/col (1-based). ok is false if out of bounds.
func (s *PTYSession) CellAt(row, col int) (terminal.Cell, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.emu.Snapshot()
	if err != nil {
		s.t.Fatalf("snapshot: %v", err)
	}
	if row < 1 || col < 1 || row > snap.Rows || col > snap.Cols {
		return terminal.Cell{}, false
	}
	idx := (row-1)*snap.Cols + (col - 1)
	if idx < 0 || idx >= len(snap.Cells) {
		return terminal.Cell{}, false
	}
	return snap.Cells[idx], true
}

// CellBG returns the background color at row/col (1-based).
func (s *PTYSession) CellBG(row, col int) (uint32, bool) {
	cell, ok := s.CellAt(row, col)
	if !ok {
		return 0, false
	}
	return cell.BG, true
}

// ExpectContains asserts that the screen contains substr.
func (s *PTYSession) ExpectContains(substr string) {
	s.t.Helper()
	screen := s.Screen()
	if !screen.Contains(substr) {
		s.t.Fatalf("expected screen to contain %q", substr)
	}
}

// ExpectRowContains asserts that the row contains substr.
func (s *PTYSession) ExpectRowContains(row int, substr string) {
	s.t.Helper()
	screen := s.Screen()
	line := screen.Row(row)
	if !strings.Contains(line, substr) {
		s.t.Fatalf("expected row %d to contain %q; got %q", row, substr, line)
	}
}

// ExpectRowNotContains asserts that the row does not contain substr.
func (s *PTYSession) ExpectRowNotContains(row int, substr string) {
	s.t.Helper()
	screen := s.Screen()
	line := screen.Row(row)
	if strings.Contains(line, substr) {
		s.t.Fatalf("expected row %d to not contain %q; got %q", row, substr, line)
	}
}

// ExpectScreen asserts a full-screen match against expected content.
func (s *PTYSession) ExpectScreen(expected string) {
	s.t.Helper()
	screen := s.Screen()
	if diff, ok := screen.Diff(expected); !ok {
		s.t.Fatalf("screen mismatch:\n%s", diff)
	}
}

// ExpectAfter waits and then asserts via the provided check.
func (s *PTYSession) ExpectAfter(d time.Duration, check func(Screen) error) {
	s.t.Helper()
	Advance(s.Clock(), d)
	screen := s.Screen()
	if err := check(screen); err != nil {
		s.t.Fatalf("%v", err)
	}
}

// Eventually retries the check until it passes or timeout elapses.
func (s *PTYSession) Eventually(timeout, step time.Duration, check func(Screen) error) {
	s.t.Helper()
	deadline := Now(s.Clock()).Add(timeout)
	for Now(s.Clock()).Before(deadline) {
		if err := check(s.Screen()); err == nil {
			return
		}
		Advance(s.Clock(), step)
	}
	if err := check(s.Screen()); err != nil {
		s.t.Fatalf("%v", err)
	}
}

func screenFromSnapshot(s terminal.Snapshot) Screen {
	lines := make([]string, s.Rows)
	for y := 0; y < s.Rows; y++ {
		var row strings.Builder
		for x := 0; x < s.Cols; x++ {
			idx := y*s.Cols + x
			if idx < 0 || idx >= len(s.Cells) {
				row.WriteRune(' ')
				continue
			}
			cell := s.Cells[idx]
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
	return Screen{Cols: s.Cols, Rows: s.Rows, Lines: lines}
}

// FormatRowDiff formats a row mismatch error.
func FormatRowDiff(name string, row int, got string) error {
	return fmt.Errorf("%s row %d mismatch: %q", name, row, got)
}
