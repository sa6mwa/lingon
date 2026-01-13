package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/pslog"
)

// Options configures a local interactive session.
type Options struct {
	Endpoint       string
	Token          string
	SessionID      string
	Cols           int
	Rows           int
	Shell          string
	Term           string
	Publish        bool
	PublishControl bool
	BufferLines    int
	Stdin          *os.File
	Stdout         *os.File
	DisableRaw     bool
	Logger         pslog.Logger
	OnPTYRead      func([]byte)
	OnPublishFrame func(*protocolpb.Frame)
	OnSnapshot     func(terminal.Snapshot)
}

// Runner executes a local interactive session with optional relay publishing.
type Runner struct {
	opts   Options
	logger pslog.Logger

	ptyFile *os.File
	ttyFile *os.File
	cmd     *exec.Cmd

	emulator terminal.Emulator
	emuMu    sync.Mutex
	writeMu  sync.Mutex

	veofMu   sync.Mutex
	veofOrig uint8
	veofSet  bool

	lastRender *protocolpb.Snapshot

	holderMu sync.Mutex
	holderID string
}

// New constructs a Runner.
func New(opts Options) *Runner {
	return &Runner{opts: opts}
}

// Run starts the local terminal session and blocks until exit.
func (r *Runner) Run(ctx context.Context) error {
	if r.opts.Logger == nil {
		r.opts.Logger = pslog.LoggerFromEnv()
	}
	r.logger = r.opts.Logger

	if r.opts.SessionID == "" {
		r.opts.SessionID = config.DefaultSessionID
	}
	if r.opts.Cols <= 0 || r.opts.Rows <= 0 {
		cols, rows := termSizeAny(r.stdout(), r.stdin())
		if cols > 0 && rows > 0 {
			r.opts.Cols, r.opts.Rows = cols, rows
		}
	}
	if r.opts.Cols <= 0 {
		r.opts.Cols = config.DefaultTerminalCols
	}
	if r.opts.Rows <= 0 {
		r.opts.Rows = config.DefaultTerminalRows
	}
	if r.opts.BufferLines <= 0 {
		r.opts.BufferLines = config.DefaultBufferLines
	}

	if r.opts.Publish && r.opts.Endpoint == "" {
		return fmt.Errorf("endpoint is required when publishing")
	}
	if r.opts.Publish && r.opts.Token == "" {
		return fmt.Errorf("access token is required when publishing")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ptyFile, ttyFile, cmd, err := r.startShell(r.opts.Shell)
	if err != nil {
		return err
	}
	r.ptyFile = ptyFile
	r.ttyFile = ttyFile
	r.cmd = cmd

	defer func() {
		_ = ptyFile.Close()
		if ttyFile != nil {
			_ = ttyFile.Close()
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	r.captureVEOF()

	r.emulator = emu.New(r.opts.Cols, r.opts.Rows)
	_ = pty.Resize(ptyFile, r.opts.Cols, r.opts.Rows)
	_ = setNonblock(ptyFile, true)
	defer func() {
		_ = setNonblock(ptyFile, false)
	}()

	stdin := r.stdin()
	stdout := r.stdout()
	if !r.opts.DisableRaw {
		if err := r.makeRaw(stdin); err != nil {
			return err
		}
		defer r.restoreTerminal(stdin)
		_ = setNonblock(stdin, true)
		defer func() {
			_ = setNonblock(stdin, false)
		}()
	} else {
		_ = setNonblock(stdin, true)
		defer func() {
			_ = setNonblock(stdin, false)
		}()
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	defer signal.Stop(sigwinch)
	go func() {
		<-sigCtx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = ptyFile.Close()
	}()
	if r.opts.Stdin != nil {
		go func() {
			<-sigCtx.Done()
			_ = stdin.Close()
		}()
	}

	var wg sync.WaitGroup
	localErr := make(chan error, 1)
	reportErr := func(err error) {
		if err == nil {
			return
		}
		select {
		case localErr <- err:
		default:
		}
	}
	var publisher *host.Publisher

	// Local input -> PTY.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-sigCtx.Done():
				return
			default:
			}
			n, err := stdin.Read(buf)
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if !errors.Is(err, io.EOF) {
					r.logger.Debug("stdin read error", "err", err)
				}
				if !r.opts.Publish {
					reportErr(err)
				}
				return
			}
			if publisher != nil {
				r.takeControl(publisher, stdout)
			}
			if _, err := r.writePTY(buf[:n]); err != nil {
				r.logger.Debug("pty write error", "err", err)
				if !r.opts.Publish {
					reportErr(err)
				}
				return
			}
		}
	}()

	// Publish relay updates (optional).
	if r.opts.Publish {
		publisher = host.NewPublisher(host.PublishOptions{
			Endpoint:       r.opts.Endpoint,
			Token:          r.opts.Token,
			SessionID:      r.opts.SessionID,
			Cols:           r.opts.Cols,
			Rows:           r.opts.Rows,
			PublishControl: r.opts.PublishControl,
			BufferLines:    r.opts.BufferLines,
			Logger:         r.logger.With("component", "publish"),
		})
		if r.opts.OnPublishFrame != nil {
			publisher.OnFrame = r.opts.OnPublishFrame
		}
		r.setHolder(host.HostControlID)
		publisher.OnInput = func(data []byte) {
			if len(data) == 0 {
				return
			}
			if r.holder() == host.HostControlID {
				return
			}
			if r.ttyFile != nil {
				data = filterRemoteInput(r.ttyFile, data)
				if len(data) == 0 {
					return
				}
			}
			_, _ = r.writePTY(data)
		}
		publisher.OnResize = func(cols, rows int) {
			if cols <= 0 || rows <= 0 {
				return
			}
			if r.holder() == host.HostControlID {
				return
			}
			r.opts.Cols, r.opts.Rows = cols, rows
			_ = pty.Resize(ptyFile, cols, rows)
			r.emuMu.Lock()
			r.emulator.Resize(cols, rows)
			r.emuMu.Unlock()
		}
		publisher.OnControl = func(holderID string) {
			if holderID == "" {
				return
			}
			r.setHolder(holderID)
		}
		go func() {
			if err := publisher.Run(sigCtx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Warn("publisher stopped", "err", err)
			}
		}()
	}
	if publisher != nil {
		publisher.TakeControl()
		if snap, err := r.snapshotLocked(); err == nil {
			publisher.Publish(nil, snap)
		}
	}

	// PTY -> emulator + local render + publish.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-sigCtx.Done():
				return
			default:
			}
			n, err := readPTY(sigCtx, ptyFile, buf)
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if !errors.Is(err, io.EOF) {
					r.logger.Debug("pty read error", "err", err)
				}
				reportErr(err)
				return
			}
			data := buf[:n]
			if r.opts.OnPTYRead != nil {
				cp := make([]byte, len(data))
				copy(cp, data)
				r.opts.OnPTYRead(cp)
			}
			r.emuMu.Lock()
			if err := r.emulator.Write(data); err != nil {
				r.logger.Debug("emulator write error", "err", err)
			}
			rawSnap, err := r.emulator.Snapshot()
			r.emuMu.Unlock()
			if err != nil {
				reportErr(err)
				return
			}
			if r.opts.OnSnapshot != nil {
				r.opts.OnSnapshot(rawSnap)
			}
			snap := protocol.SnapshotToProto(rawSnap)
			if r.useRenderer(stdout) {
				cols, rows := termSizeAny(stdout, stdin)
				if cols <= 0 || rows <= 0 {
					cols, rows = r.opts.Cols, r.opts.Rows
				}
				if err := render.SnapshotViewportDelta(stdout, r.lastRender, snap, cols, rows); err != nil {
					r.logger.Debug("render error", "err", err)
				}
				r.lastRender = snap
			} else {
				r.lastRender = nil
				if err := writeAll(sigCtx, stdout, data); err != nil {
					r.logger.Debug("stdout write error", "err", err)
				}
			}
			if publisher != nil {
				publisher.Publish(data, snap)
			}
		}
	}()

	// Resize handling (local terminal size changes).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-sigwinch:
				cols, rows := termSizeAny(stdout, stdin)
				if cols <= 0 || rows <= 0 {
					continue
				}
				if publisher != nil {
					r.takeControl(publisher, stdout)
				}
				r.opts.Cols, r.opts.Rows = cols, rows
				_ = pty.Resize(ptyFile, cols, rows)
				r.emuMu.Lock()
				r.emulator.Resize(cols, rows)
				rawSnap, err := r.emulator.Snapshot()
				r.emuMu.Unlock()
				if err == nil {
					if publisher != nil {
						publisher.Resize(cols, rows, protocol.SnapshotToProto(rawSnap))
					}
				}
			}
		}
	}()

	select {
	case <-sigCtx.Done():
	case <-waitProcess(cmd):
	case <-localErr:
	}

	cancel()
	wg.Wait()
	return nil
}

func (r *Runner) startShell(shellOverride string) (*os.File, *os.File, *exec.Cmd, error) {
	path := shellOverride
	if path == "" {
		if u, err := user.Current(); err == nil && u != nil && u.Uid != "" {
			if shell, err := shellFromPasswd(u.Uid); err == nil && shell != "" {
				path = shell
			}
		}
	}
	if path == "" {
		path = os.Getenv("SHELL")
	}
	if path == "" {
		path = "/bin/sh"
	}
	cmd := exec.Command(path)
	if r.opts.Term != "" {
		cmd.Env = append(os.Environ(), "TERM="+r.opts.Term)
	}
	ptyFile, ttyFile, err := pty.StartWithTTY(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	return ptyFile, ttyFile, cmd, nil
}

func (r *Runner) makeRaw(file *os.File) error {
	if file == nil {
		return fmt.Errorf("stdin is nil")
	}
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return fmt.Errorf("stdin is not a terminal")
	}
	storeTerminalState(state)
	return nil
}

func (r *Runner) restoreTerminal(file *os.File) {
	state := loadTerminalState()
	if state != nil {
		_ = term.Restore(int(file.Fd()), state)
	}
}

func (r *Runner) snapshotLocked() (*protocolpb.Snapshot, error) {
	r.emuMu.Lock()
	defer r.emuMu.Unlock()
	snap, err := r.emulator.Snapshot()
	if err != nil {
		return nil, err
	}
	return protocol.SnapshotToProto(snap), nil
}

func (r *Runner) writePTY(data []byte) (int, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.ptyFile == nil {
		return 0, fmt.Errorf("pty not initialized")
	}
	return r.ptyFile.Write(data)
}

func (r *Runner) stdin() *os.File {
	if r.opts.Stdin != nil {
		return r.opts.Stdin
	}
	return os.Stdin
}

func (r *Runner) stdout() *os.File {
	if r.opts.Stdout != nil {
		return r.opts.Stdout
	}
	return os.Stdout
}

func termSize(file *os.File) (int, int) {
	if file == nil {
		return 0, 0
	}
	cols, rows, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0, 0
	}
	return cols, rows
}

func setNonblock(file *os.File, on bool) error {
	if file == nil {
		return nil
	}
	return syscall.SetNonblock(int(file.Fd()), on)
}

func waitProcess(cmd *exec.Cmd) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(ch)
	}()
	return ch
}

func writeAll(ctx context.Context, w io.Writer, data []byte) error {
	for len(data) > 0 {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				if ctx != nil {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(5 * time.Millisecond):
					}
				} else {
					time.Sleep(5 * time.Millisecond)
				}
				continue
			}
			return err
		}
		if n == 0 {
			if ctx != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Millisecond):
				}
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	return nil
}

func (r *Runner) setHolder(holderID string) {
	r.holderMu.Lock()
	r.holderID = holderID
	r.holderMu.Unlock()
	r.applyVEOF(holderID)
}

func (r *Runner) holder() string {
	r.holderMu.Lock()
	defer r.holderMu.Unlock()
	return r.holderID
}

func (r *Runner) captureVEOF() {
	if r.ttyFile == nil {
		return
	}
	val, err := getVEOF(r.ttyFile)
	if err != nil {
		return
	}
	r.veofMu.Lock()
	r.veofOrig = val
	r.veofSet = true
	r.veofMu.Unlock()
}

func (r *Runner) applyVEOF(holderID string) {
	if r.ttyFile == nil {
		return
	}
	r.veofMu.Lock()
	if !r.veofSet {
		r.veofMu.Unlock()
		return
	}
	orig := r.veofOrig
	r.veofMu.Unlock()
	target := orig
	if holderID != "" && holderID != host.HostControlID {
		target = 0
	}
	_ = setVEOF(r.ttyFile, target)
}

func (r *Runner) useRenderer(stdout *os.File) bool {
	cols, rows := termSizeAny(stdout, r.stdin())
	if cols <= 0 || rows <= 0 {
		return false
	}
	return cols != r.opts.Cols || rows != r.opts.Rows
}

func (r *Runner) takeControl(publisher *host.Publisher, stdout *os.File) {
	if publisher == nil {
		return
	}
	publisher.TakeControl()
	r.setHolder(host.HostControlID)

	cols, rows := termSizeAny(stdout, r.stdin())
	if cols <= 0 || rows <= 0 {
		return
	}
	if cols == r.opts.Cols && rows == r.opts.Rows {
		return
	}
	r.opts.Cols, r.opts.Rows = cols, rows
	_ = pty.Resize(r.ptyFile, cols, rows)
	r.emuMu.Lock()
	r.emulator.Resize(cols, rows)
	snap, err := r.emulator.Snapshot()
	r.emuMu.Unlock()
	if err == nil {
		publisher.Resize(cols, rows, protocol.SnapshotToProto(snap))
	}
}

func termSizeAny(files ...*os.File) (int, int) {
	for _, file := range files {
		if file == nil {
			continue
		}
		cols, rows := termSize(file)
		if cols > 0 && rows > 0 {
			return cols, rows
		}
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer func() {
			_ = tty.Close()
		}()
		if cols, rows := termSize(tty); cols > 0 && rows > 0 {
			return cols, rows
		}
	}
	return 0, 0
}

var terminalStateMu sync.Mutex
var terminalState *term.State

func storeTerminalState(state *term.State) {
	terminalStateMu.Lock()
	terminalState = state
	terminalStateMu.Unlock()
}

func loadTerminalState() *term.State {
	terminalStateMu.Lock()
	defer terminalStateMu.Unlock()
	return terminalState
}
