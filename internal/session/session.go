package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/control"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/theme"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

// Options configures a local interactive session.
type Options struct {
	Endpoint         string
	Token            string
	AuthFile         string
	SessionID        string
	SessionName      string
	Cols             int
	Rows             int
	Shell            string
	Term             string
	Respawn          bool
	Offline          bool
	Theme            string
	Publish          bool
	PublishControl   bool
	HostnameOnly     bool
	ScrollbackLines  int
	MaxReplayScreens int
	TLSDir           string
	Insecure         bool
	Stdin            *os.File
	Stdout           *os.File
	DisableRaw       bool
	Logger           pslog.Logger
	// Clock controls time-based behavior (reconnects, overlays).
	Clock           clock.Clock
	OnPTYRead       func([]byte)
	OnPublishFrame  func(*protocolpb.Frame)
	OnPublishStatus func(PublishStatus)
	OnPublishWall   func(*protocolpb.Wall)
	OnStatus        func(StatusUpdate)
	// DisableDesktopNotifications suppresses best-effort desktop notifications for inactivity walls.
	DisableDesktopNotifications bool
	DesktopNotifier             desktopnotify.Notifier
	// ToggleWallInactivityFallback handles local-only wall inactivity cycling
	// when relay-backed toggle is unavailable.
	ToggleWallInactivityFallback func(context.Context, string) (WallInactivityToggleResult, error)
	OnSnapshot                   func(terminal.Snapshot)
	Trace                        *trace.Writer
}

// PublishStatusKind identifies host publish connectivity transitions.
type PublishStatusKind string

const (
	// PublishStatusConnected indicates relay connectivity was restored.
	PublishStatusConnected PublishStatusKind = "connected"
	// PublishStatusConnectionLost indicates relay connectivity was lost.
	PublishStatusConnectionLost PublishStatusKind = "connection_lost"
	// PublishStatusConnectionBackoff indicates reconnect backoff countdown is active.
	PublishStatusConnectionBackoff PublishStatusKind = "connection_backoff"
)

// PublishStatus describes a host publish connectivity state change.
type PublishStatus struct {
	SessionID string
	Kind      PublishStatusKind
	Message   string
	Endpoint  string
	Remaining time.Duration
}

// StatusKind identifies generic status banner kinds.
type StatusKind string

const (
	// StatusKindInfo maps to a non-error status banner.
	StatusKindInfo StatusKind = "info"
	// StatusKindError maps to an error status banner.
	StatusKindError StatusKind = "error"
)

// StatusUpdate describes a transient status banner update.
type StatusUpdate struct {
	SessionID string
	Kind      StatusKind
	Message   string
	Duration  time.Duration
}

// WallInactivityToggleResult describes post-toggle wall inactivity state.
type WallInactivityToggleResult struct {
	Enabled       bool
	InactiveAfter string
}

// Runner executes a local interactive session with optional relay publishing.
type Runner struct {
	opts        Options
	logger      pslog.Logger
	runCtx      context.Context
	stdoutMu    sync.Mutex
	stopOnce    sync.Once
	stopFunc    context.CancelFunc
	compositor  *mvu.Runtime
	renderCache mvu.RenderCache

	sessionName string
	sessionBase string

	localMu        sync.RWMutex
	localSessions  map[string]*localSession
	localOrder     []string
	sessionOrder   []string
	localClosed    map[string]bool
	localSeq       int
	sessionOrderMu sync.RWMutex

	effects     *mvu.EffectScheduler
	tabSuppress mvu.SessionTabSuppression

	viewMu          sync.RWMutex
	activeSessionID string
	activeIsLocal   bool
	remoteSessions  *remoteManager
	clock           clock.Clock
	trace           *trace.Writer

	renderCursorMu      sync.Mutex
	renderCursorRow     int
	renderCursorCol     int
	renderCursorVisible bool

	inputTraceMu      sync.Mutex
	inputTraceEnter   map[string]time.Time
	inputTracePending map[string]bool
	inputPrefillMu    sync.Mutex
	inputPrefill      []byte

	outerDefaultFg     string
	outerDefaultBg     string
	outerDefaultCursor string
	outerOscMu         sync.Mutex
	outerOscPending    map[int]bool
	outerOscDeadline   time.Time
	outerOscGraceUntil time.Time
	outerOscParser     oscStreamParser

	scrollbackMu      sync.Mutex
	scrollbackView    mvu.ScrollbackViewport
	scrollbackSession string

	wallNotifyMu    sync.Mutex
	wallNotifyAfter map[string]time.Duration
	wallNotifyTimer map[string]*clock.Timer
	wallNotifyArmed map[string]bool

	tokenRefresher func(context.Context) (string, error)

	themeName string
}

// New constructs a Runner.
func New(opts Options) *Runner {
	if opts.Clock == nil {
		opts.Clock = clock.New()
	}
	compositor := mvu.NewRuntime()
	compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:    opts.Clock,
		Endpoint: opts.Endpoint,
		Theme:    theme.TUI(resolveThemeName(opts.Theme)),
	}})
	return &Runner{opts: opts, clock: opts.Clock, trace: opts.Trace, compositor: compositor}
}

func (r *Runner) runtime() *mvu.Runtime {
	if r.compositor != nil {
		return r.compositor
	}
	compositor := mvu.NewRuntime()
	clk := r.clock
	if clk == nil {
		clk = r.opts.Clock
	}
	themeName := r.themeName
	if themeName == "" {
		themeName = resolveThemeName(r.opts.Theme)
	}
	compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:    clk,
		Endpoint: r.opts.Endpoint,
		Theme:    theme.TUI(themeName),
	}})
	r.compositor = compositor
	return compositor
}

// SessionID returns the active session ID.
func (r *Runner) SessionID() string {
	return r.opts.SessionID
}

func (r *Runner) initializeSessionIdentity() {
	autoNamed := false
	if r.opts.SessionID == "" {
		if r.opts.Publish {
			r.opts.SessionID, r.sessionName = defaultSessionIdentity()
			autoNamed = true
		} else {
			r.opts.SessionID = config.DefaultSessionID
		}
	}
	if r.sessionName == "" {
		if r.opts.SessionName != "" {
			r.sessionName = r.opts.SessionName
		} else if r.opts.SessionID != "" {
			r.sessionName = r.opts.SessionID
		} else if r.opts.Publish {
			r.sessionName = defaultSessionName()
		}
	}
	if r.sessionName != "" {
		r.sessionBase = r.sessionName
	} else if r.opts.SessionID != "" {
		r.sessionBase = r.opts.SessionID
	} else {
		r.sessionBase = defaultSessionName()
	}
	if autoNamed {
		r.sessionBase = defaultSessionName()
		r.localSeq = parseSessionSequenceSuffix(r.sessionName)
	} else {
		r.localSeq = 1
		if seq := parseSessionSequenceSuffix(r.sessionName); seq > r.localSeq {
			r.localSeq = seq
		}
		if seq := parseSessionSequenceSuffix(r.opts.SessionID); seq > r.localSeq {
			r.localSeq = seq
		}
	}
}

// Run starts the local terminal session and blocks until exit.
func (r *Runner) Run(ctx context.Context) error {
	if r.opts.Logger == nil {
		r.opts.Logger = logging.Default()
	}
	r.logger = r.opts.Logger
	if r.opts.Clock == nil {
		r.opts.Clock = clock.New()
	}
	r.clock = r.opts.Clock
	r.initializeSessionIdentity()
	ui := r.runtime()
	ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Clock: r.clock, Endpoint: r.opts.Endpoint}})
	r.effects = mvu.NewEffectScheduler(r.clock)
	defer r.effects.StopAll()
	defer r.stopLocalWallNotifications()
	r.applyTheme(resolveThemeName(r.opts.Theme))
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
	if r.opts.MaxReplayScreens <= 0 {
		r.opts.MaxReplayScreens = 10
	}
	if r.opts.ScrollbackLines <= 0 {
		r.opts.ScrollbackLines = config.DefaultScrollbackLines
	}

	if r.opts.Publish && r.opts.Endpoint == "" {
		return fmt.Errorf("endpoint is required when publishing")
	}

	var tokenRefresher func(context.Context) (string, error)
	if r.opts.Publish && r.opts.AuthFile != "" && r.opts.Endpoint != "" {
		tokenRefresher = relayclient.TokenRefresher(r.opts.Endpoint, r.opts.AuthFile, r.opts.TLSDir, r.opts.Insecure, func(token string) {
			r.opts.Token = token
		})
		if token, err := tokenRefresher(ctx); err == nil && token != "" {
			r.opts.Token = token
		} else if err != nil && r.logger != nil {
			r.logger.Debug("session.auth.refresh.failed", "err", err)
		}
	}
	if r.opts.Publish && r.opts.Token == "" && tokenRefresher == nil {
		return fmt.Errorf("access token is required when publishing")
	}
	r.tokenRefresher = tokenRefresher

	ctx, cancel := context.WithCancel(ctx)
	r.stopFunc = cancel
	defer cancel()

	stdin := r.stdin()
	stdout := r.stdout()
	defer restoreCursor(stdout, r.clock)
	if enterAltScreen(stdout, r.clock) {
		defer exitAltScreen(stdout, r.clock)
	}
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

	if isTerminalWriter(stdout) && term.IsTerminal(int(stdin.Fd())) {
		defaults, prefill := probeOuterColors(ctx, stdout, stdin, r.clock, r.trace)
		r.outerDefaultFg = defaults.fg
		r.outerDefaultBg = defaults.bg
		r.outerDefaultCursor = defaults.cursor
		r.addInputPrefill(prefill)
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	r.runCtx = sigCtx

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	defer signal.Stop(sigwinch)
	if r.opts.Stdin != nil {
		go func() {
			<-sigCtx.Done()
			_ = stdin.Close()
		}()
	}

	var wg sync.WaitGroup
	var gate *netgate.Gate
	debugRemoteInput := os.Getenv("LINGON_DEBUG_INPUT") == "1"

	if r.opts.Publish {
		gate = netgate.New(r.clock)
	}

	r.localSessions = make(map[string]*localSession)
	r.localClosed = make(map[string]bool)
	if err := r.startInitialLocalSession(sigCtx, tokenRefresher, gate, stdout, stdin, debugRemoteInput); err != nil {
		return err
	}

	if r.opts.Publish {
		r.remoteSessions = newRemoteManager(remoteOptions{
			Endpoint:        r.opts.Endpoint,
			Token:           r.opts.Token,
			TokenRefresher:  tokenRefresher,
			HostnameOnly:    r.opts.HostnameOnly,
			TLSDir:          r.opts.TLSDir,
			Insecure:        r.opts.Insecure,
			Theme:           r.themeName,
			Logger:          r.logger,
			Compositor:      r.runtime(),
			TermSize:        func() (int, int) { return termSizeAny(stdout, stdin) },
			Clock:           r.clock,
			InactiveTTL:     30 * time.Second,
			RefreshInterval: 60 * time.Second,
			Gate:            gate,
			OnSessions: func(sessions []remoteSessionInfo) {
				r.handleSessionListUpdate(sessions, stdout, stdin)
			},
			OnViewClosed: func(id string, err error) {
				activeID, _ := r.activeSession()
				if id != "" && id == activeID {
					if r.remoteSessions != nil && r.remoteSessions.IsDisabled(id) {
						return
					}
					r.activateAnyLocal(stdout, stdin)
				}
				if err != nil && !errors.Is(err, context.Canceled) {
					r.logger.Debug("session.remote.view.closed", "session", id, "err", err)
				}
			},
			OnOverlayChange: func() {
				activeID, activeLocal := r.activeSession()
				if activeID == "" {
					return
				}
				if activeLocal {
					r.forceRedraw(stdout)
					return
				}
				if r.remoteSessions == nil {
					return
				}
				if r.remoteSessions.IsDisabled(activeID) {
					r.remoteSessions.RenderDisabled(activeID, stdout)
					return
				}
				r.remoteSessions.Render(activeID)
			},
		})
		r.remoteSessions.Start(sigCtx)
		r.handleSessionListUpdate(r.remoteSessions.Sessions(), stdout, stdin)
	}

	// Local input -> PTY.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		var prefix control.Prefix
		pending := make([]byte, 0, 4096)
		var scrollState scrollInputState
		for {
			select {
			case <-sigCtx.Done():
				return
			default:
			}
			n, ok := r.consumeInputPrefill(buf)
			if !ok {
				var err error
				n, err = readInput(sigCtx, stdin, buf)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
						r.clock.Sleep(10 * time.Millisecond)
						continue
					}
					if !errors.Is(err, io.EOF) {
						r.logger.Debug("session.stdin.read.failed", "err", err)
					}
					if !r.opts.Publish {
						r.requestStop()
					}
					return
				}
			}
			if n == 0 {
				continue
			}
			if r.isLocalActive() {
				if local := r.activeLocalSession(); local != nil {
					r.takeControlLocal(local, stdout, stdin)
				}
			}
			filtered := r.filterOuterOSC(buf[:n])
			if len(filtered) == 0 {
				continue
			}
			if activeID, activeLocal := r.activeSession(); activeLocal {
				if local := r.localSession(activeID); local != nil {
					if snap := local.Snapshot(); snap != nil {
						filtered = terminal.TranslateAppCursorKeys(filtered, snap.Mode&terminal.SnapshotModeAppCursor != 0)
					}
				}
			}
			pending = pending[:0]
			flushPending := func() bool {
				if len(pending) == 0 {
					return true
				}
				activeID, activeLocal := r.activeSession()
				if activeLocal {
					local := r.localSession(activeID)
					if local == nil {
						return true
					}
					for {
						if _, err := local.writePTY(pending); err != nil {
							if errors.Is(err, errPTYNotReady) {
								if r.logger != nil {
									r.logger.Debug("session.pty.write.waiting", "session", activeID)
								}
								select {
								case <-sigCtx.Done():
									return false
								default:
								}
								r.clock.Sleep(10 * time.Millisecond)
								continue
							}
							r.logger.Debug("session.pty.write.failed", "err", err, "session", activeID)
							return false
						}
						break
					}
					if bytes.IndexByte(pending, control.CtrlL) >= 0 {
						r.setTabSuppressed(activeID, true)
						r.forceRedraw(stdout)
					} else {
						r.setTabSuppressed(activeID, false)
					}
					r.noteLocalActivity(activeID)
					r.noteLocalEnterInput(activeID, pending)
				} else if r.remoteSessions != nil {
					if bytes.IndexByte(pending, control.CtrlL) >= 0 {
						r.setTabSuppressed(activeID, true)
					} else {
						r.setTabSuppressed(activeID, false)
					}
					if err := r.remoteSessions.SendInput(sigCtx, activeID, pending, stdout); err != nil {
						r.logger.Debug("session.remote.input.failed", "session", activeID, "err", err)
					}
				}
				pending = pending[:0]
				return true
			}
			processNormalByte := func(b byte) bool {
				helpVisible := r.runtime().Read().HelpVisible
				if uiAction, ok := mvu.ActionForHelpDismissKey(helpVisible, b); ok {
					r.runtime().ApplyAction(uiAction)
					r.forceRedraw(stdout)
					return true
				}
				// Help modal is input-modal: underlying session keeps rendering,
				// but keys are swallowed until help is dismissed.
				if helpVisible {
					return true
				}
				activeID, activeLocal := r.activeSession()
				action, out := prefix.Feed(b)
				if action != control.ActionNone {
					if !flushPending() {
						return false
					}
					r.setTabSuppressed(activeID, false)
					switch action {
					case control.ActionHelp:
						r.runtime().ApplyAction(mvu.HelpVisibleAction{Visible: true})
						r.forceRedraw(stdout)
						return true
					case control.ActionToggleTabBar:
						r.runtime().ApplyAction(mvu.TabToggleAction{})
						r.forceRedraw(stdout)
						return true
					}
					if uiAction, ok := mvu.ActionForControl(action); ok {
						r.runtime().ApplyAction(uiAction)
						r.forceRedraw(stdout)
						return true
					}
					switch action {
					case control.ActionQuit:
						if !activeLocal {
							r.showStatus("cannot close local session while remote tab active", stdout, 2*time.Second)
							return true
						}
						r.closeLocalSession(activeID, stdout, stdin)
					case control.ActionSendCtrlD:
						eof := []byte{0x04}
						if activeLocal {
							local := r.localSession(activeID)
							if local == nil {
								return true
							}
							for {
								if _, err := local.writePTY(eof); err != nil {
									if errors.Is(err, errPTYNotReady) {
										if r.logger != nil {
											r.logger.Debug("session.pty.write.waiting", "session", activeID)
										}
										select {
										case <-sigCtx.Done():
											return false
										default:
										}
										r.clock.Sleep(10 * time.Millisecond)
										continue
									}
									r.logger.Debug("session.pty.write.failed", "err", err, "session", activeID)
									return true
								}
								break
							}
							r.noteLocalEnterInput(activeID, eof)
							return true
						}
						if r.remoteSessions != nil {
							if err := r.remoteSessions.SendCommand(sigCtx, activeID, protocolpb.CommandKind_COMMAND_KIND_SEND_EOF, stdout); err != nil {
								r.logger.Debug("session.remote.command.failed", "session", activeID, "kind", protocolpb.CommandKind_COMMAND_KIND_SEND_EOF.String(), "err", err)
							}
						}
					case control.ActionNewPTY:
						r.createLocalSession(sigCtx, tokenRefresher, gate, stdout, stdin, debugRemoteInput)
					case control.ActionScrollback:
						r.enterScrollback(activeID, stdout, stdin)
					case control.ActionToggleRespawn:
						if !activeLocal {
							r.showStatus("respawn toggle is local-only", stdout, 2*time.Second)
							return true
						}
						r.toggleRespawn(activeID, stdout)
					case control.ActionToggleOffline:
						if !activeLocal {
							r.showErrorStatus("offline toggle is host local-only", stdout, 2*time.Second)
							return true
						}
						r.toggleOffline(activeID, stdout)
					case control.ActionToggleWallInactivity:
						r.toggleWallInactivity(sigCtx, activeID, tokenRefresher, stdout)
					case control.ActionNextTab:
						r.switchTab(sigCtx, 1, stdout, stdin)
					case control.ActionPrevTab:
						r.switchTab(sigCtx, -1, stdout, stdin)
					case control.ActionNextTheme:
						r.cycleTheme(stdout)
					}
					return true
				}
				if len(out) > 0 {
					pending = append(pending, out...)
				}
				return true
			}
			for _, b := range filtered {
				activeID, _ := r.activeSession()
				if r.scrollbackActiveFor(activeID) {
					cmd := scrollState.feed(b)
					if cmd == scrollExit {
						r.exitScrollback(stdout, stdin)
						continue
					}
					if cmd != scrollNone {
						_, rows := termSizeAny(stdout, stdin)
						if rows <= 0 {
							rows = config.DefaultTerminalRows
						}
						half := rows / 2
						if half < 1 {
							half = 1
						}
						switch cmd {
						case scrollPageUp:
							r.scrollbackPage(1, half, stdout, stdin)
						case scrollPageDown:
							r.scrollbackPage(-1, half, stdout, stdin)
						case scrollLineUp:
							r.scrollbackPage(1, 1, stdout, stdin)
						case scrollLineDown:
							r.scrollbackPage(-1, 1, stdout, stdin)
						case scrollFiveUp:
							r.scrollbackPage(1, 5, stdout, stdin)
						case scrollFiveDown:
							r.scrollbackPage(-1, 5, stdout, stdin)
						case scrollTop:
							r.scrollbackTop(rows, stdout, stdin)
						case scrollBottom:
							r.scrollbackBottom(stdout, stdin)
						case scrollWheelUp:
							r.scrollbackPage(1, 3, stdout, stdin)
						case scrollWheelDown:
							r.scrollbackPage(-1, 3, stdout, stdin)
						}
					}
					continue
				}
				if !processNormalByte(b) {
					return
				}
			}
			if !flushPending() {
				return
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
				if r.isLocalActive() {
					r.opts.Cols, r.opts.Rows = cols, rows
					activeID, _ := r.activeSession()
					if local := r.activeLocalSession(); local != nil {
						if snap, err := local.Resize(cols, rows); err == nil && local.publisher != nil {
							local.publisher.Resize(cols, rows, snap)
						}
					}
					if r.scrollbackActiveFor(activeID) {
						r.renderScrollback(stdout, stdin)
					}
					continue
				}
				if r.remoteSessions != nil {
					activeID, _ := r.activeSession()
					_ = r.remoteSessions.SendResize(sigCtx, activeID, cols, rows)
					r.remoteSessions.Render(activeID)
				}
			}
		}
	}()

	<-sigCtx.Done()

	cancel()
	wg.Wait()
	r.clearOverlays(stdout, stdin)
	return nil
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

func (r *Runner) forceRedraw(stdout *os.File) {
	activeID, _ := r.activeSession()
	if r.scrollbackActiveFor(activeID) {
		r.renderScrollback(stdout, r.stdin())
		return
	}
	r.forceRedrawWithMode(stdout, false)
}

func (r *Runner) forceRedrawWithMode(stdout *os.File, forceFull bool) {
	if !r.isLocalActive() {
		if r.remoteSessions != nil {
			activeID, _ := r.activeSession()
			if r.remoteSessions.IsDisabled(activeID) {
				r.remoteSessions.RenderDisabled(activeID, stdout)
			} else {
				r.remoteSessions.Render(activeID)
			}
		}
		return
	}
	local := r.activeLocalSession()
	if local == nil {
		return
	}
	activeID, _ := r.activeSession()
	suppressTabs := r.tabSuppressed(activeID)
	snap := local.Snapshot()
	if snap == nil {
		cols, rows := termSizeAny(stdout, r.stdin())
		if cols <= 0 || rows <= 0 {
			cols, rows = r.opts.Cols, r.opts.Rows
		}
		snap = mvu.BlankSnapshot(cols, rows)
	}
	cols, rows := termSizeAny(stdout, r.stdin())
	if cols <= 0 || rows <= 0 {
		cols, rows = int(snap.Cols), int(snap.Rows)
	}
	_ = r.renderHostMVU(context.Background(), stdout, snap, cols, rows, forceFull, suppressTabs)
}

func (r *Runner) enterScrollback(sessionID string, stdout, stdin *os.File) {
	r.scrollbackMu.Lock()
	r.scrollbackView.Enter()
	r.scrollbackSession = sessionID
	r.scrollbackMu.Unlock()

	if activeID, isLocal := r.activeSession(); activeID == sessionID && !isLocal && r.remoteSessions != nil {
		r.remoteSessions.SetScrollbackActive(sessionID, true)
		return
	}
	r.renderScrollback(stdout, stdin)
}

func (r *Runner) exitScrollback(stdout, stdin *os.File) {
	r.scrollbackMu.Lock()
	active := r.scrollbackView.Active()
	sessionID := r.scrollbackSession
	r.scrollbackView.Exit()
	r.scrollbackSession = ""
	r.scrollbackMu.Unlock()
	if !active {
		return
	}
	if r.remoteSessions != nil && sessionID != "" {
		r.remoteSessions.ResetScrollback(sessionID)
	}
	r.runtime().ApplyAction(mvu.ScrollbackPercentAction{Visible: false})
	r.forceRedrawWithMode(stdout, true)
}

func (r *Runner) scrollbackPage(delta int, stepRows int, stdout, stdin *os.File) {
	sessionID, isLocal := r.activeSession()
	if sessionID == "" {
		return
	}
	if !isLocal {
		if r.remoteSessions != nil {
			r.remoteSessions.ScrollbackPage(sessionID, delta, stepRows)
		}
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	scrollback := local.scrollbackSnapshot()
	snap := local.Snapshot()
	if snap == nil {
		return
	}
	_, rows := termSizeAny(stdout, stdin)
	if rows <= 0 {
		rows = int(snap.Rows)
	}
	if rows <= 0 {
		return
	}
	if stepRows <= 0 {
		stepRows = rows
	}
	totalRows := len(scrollback) + int(snap.Rows)
	r.scrollbackMu.Lock()
	changed := r.scrollbackView.Page(totalRows, rows, delta, stepRows)
	r.scrollbackMu.Unlock()
	if !changed {
		return
	}
	r.renderScrollback(stdout, stdin)
}

func (r *Runner) scrollbackTop(viewRows int, stdout, stdin *os.File) {
	sessionID, isLocal := r.activeSession()
	if sessionID == "" {
		return
	}
	if viewRows <= 0 {
		_, rows := termSizeAny(stdout, stdin)
		if rows <= 0 {
			return
		}
		viewRows = rows
	}
	if !isLocal {
		if r.remoteSessions != nil {
			r.remoteSessions.ScrollbackTop(sessionID, viewRows)
		}
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	scrollback := local.scrollbackSnapshot()
	snap := local.Snapshot()
	if snap == nil {
		return
	}
	totalRows := len(scrollback) + int(snap.Rows)
	r.scrollbackMu.Lock()
	r.scrollbackView.Top(totalRows, viewRows)
	r.scrollbackMu.Unlock()
	r.renderScrollback(stdout, stdin)
}

func (r *Runner) scrollbackBottom(stdout, stdin *os.File) {
	sessionID, isLocal := r.activeSession()
	if sessionID == "" {
		return
	}
	if !isLocal {
		if r.remoteSessions != nil {
			r.remoteSessions.ScrollbackBottom(sessionID)
		}
		return
	}
	r.scrollbackMu.Lock()
	if r.scrollbackView.Active() && r.scrollbackSession == sessionID {
		r.scrollbackView.Bottom()
	}
	r.scrollbackMu.Unlock()
	r.renderScrollback(stdout, stdin)
}

func (r *Runner) renderScrollback(stdout, stdin *os.File) {
	sessionID, isLocal := r.activeSession()
	if sessionID == "" {
		return
	}
	r.scrollbackMu.Lock()
	active := r.scrollbackView.Active() && r.scrollbackSession == sessionID
	offset := r.scrollbackView.Offset()
	r.scrollbackMu.Unlock()
	if !active {
		return
	}
	if !isLocal {
		if r.remoteSessions != nil {
			r.remoteSessions.Render(sessionID)
		}
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	snap := local.Snapshot()
	if snap == nil {
		return
	}
	scrollback := local.scrollbackSnapshot()
	cols, rows := termSizeAny(stdout, stdin)
	if cols <= 0 || rows <= 0 {
		cols = int(snap.Cols)
		rows = int(snap.Rows)
	}
	percent := r.scrollbackView.Percent(len(scrollback)+int(snap.Rows), rows)
	r.runtime().ApplyAction(mvu.ScrollbackPercentAction{Visible: true, Percent: percent})
	viewSnap := mvu.BuildScrollbackViewFromTerminal(cols, rows, scrollback, snap, offset)
	ctx := r.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.renderSnapshotWithOverlays(ctx, stdout, stdin, viewSnap); err != nil && r.logger != nil {
		r.logger.Debug("session.scrollback.render.failed", "err", err, "session", sessionID)
	}
}

func (r *Runner) renderSnapshotWithOverlays(ctx context.Context, stdout, stdin *os.File, snap *protocolpb.Snapshot) error {
	if snap == nil {
		return nil
	}
	activeID, _ := r.activeSession()
	suppressTabs := r.tabSuppressed(activeID)
	cols, rows := termSizeAny(stdout, stdin)
	if cols <= 0 || rows <= 0 {
		cols, rows = r.opts.Cols, r.opts.Rows
	}
	return r.renderHostMVU(ctx, stdout, snap, cols, rows, false, suppressTabs)
}

func (r *Runner) renderHostMVU(ctx context.Context, stdout *os.File, snap *protocolpb.Snapshot, cols, rows int, forceFull, suppressTabs bool) error {
	if snap == nil {
		return nil
	}
	ui := r.runtime()
	now := r.clock.Now()
	cursor := mvu.CursorFromSnapshot(snap, cols, rows)
	r.stdoutMu.Lock()
	defer r.stdoutMu.Unlock()
	frame, err := ui.RenderHostFrame(mvu.RuntimeHostFrameInput{
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       cursor,
		Now:          now,
		ForceFull:    forceFull,
		SuppressTabs: suppressTabs,
		Cache:        &r.renderCache,
	})
	if err != nil {
		return err
	}
	rendered := frame.Rendered
	renderCursor := cursor
	if rendered.ComposedSnapshot != nil {
		renderCursor = mvu.CursorFromSnapshot(rendered.ComposedSnapshot, cols, rows)
	}
	row := renderCursor.Row
	col := renderCursor.Col
	visible := renderCursor.Visible
	r.renderCursorMu.Lock()
	r.renderCursorRow = row
	r.renderCursorCol = col
	r.renderCursorVisible = visible
	r.renderCursorMu.Unlock()
	if r.trace != nil {
		activeID, activeLocal := r.activeSession()
		r.trace.Event("render", map[string]any{
			"component":           "host",
			"session_id":          activeID,
			"active_local":        activeLocal,
			"overlay":             true,
			"help_visible":        rendered.Resolved.HelpVisible,
			"wall_visible":        rendered.Resolved.WallVisible,
			"tab_bar_visible":     rendered.Resolved.TabBarVisible,
			"top_overlay_visible": rendered.Resolved.TopOverlayVisible,
			"top_overlay_on_row":  rendered.Resolved.TopOverlayVisible,
			"cursor_row":          row,
			"cursor_col":          col,
			"cursor_visible":      visible,
			"cols":                cols,
			"rows":                rows,
		})
	}
	if err := writeAll(ctx, stdout, rendered.Bytes, r.clock); err != nil {
		return err
	}
	runCtx := r.runCtx
	if runCtx != nil {
		r.scheduleRedrawEffect(runCtx, mvu.EffectKeyTabAutoHide, stdout, frame.TabDelay, false)
		stateFull := rendered.Resolved.DisconnectVisible
		r.scheduleRedrawEffect(runCtx, mvu.EffectKeyStateExpiry, stdout, frame.StateDelay, stateFull)
	}
	return nil
}

func (r *Runner) requestStop() {
	r.stopOnce.Do(func() {
		if r.stopFunc != nil {
			r.stopFunc()
		}
	})
}

func (r *Runner) clearOverlays(stdout *os.File, stdin *os.File) {
	ui := r.runtime()
	if !ui.ApplyAction(mvu.ClearOverlaysAction{}).Changed {
		return
	}
	if r.effects != nil {
		r.effects.Stop(mvu.EffectKeyStateExpiry)
		r.effects.Stop(mvu.EffectKeyTabAutoHide)
	}
	var snap *protocolpb.Snapshot
	if r.isLocalActive() {
		if local := r.activeLocalSession(); local != nil {
			snap = local.Snapshot()
		}
	}
	if snap == nil {
		return
	}
	cols, rows := termSizeAny(stdout, stdin)
	if cols <= 0 || rows <= 0 {
		cols, rows = int(snap.Cols), int(snap.Rows)
	}
	cursor := mvu.CursorFromSnapshot(snap, cols, rows)
	frame, err := ui.RenderHostFrame(mvu.RuntimeHostFrameInput{
		Snapshot:  snap,
		Cols:      cols,
		Rows:      rows,
		Cursor:    cursor,
		Now:       r.clock.Now(),
		ForceFull: snap.Cols != uint32(cols) || snap.Rows != uint32(rows),
		Cache:     &r.renderCache,
	})
	if err != nil {
		return
	}
	rendered := frame.Rendered
	r.stdoutMu.Lock()
	defer r.stdoutMu.Unlock()
	_ = writeAll(context.Background(), stdout, rendered.Bytes, r.clock)
}

func (r *Runner) scheduleRedrawEffect(ctx context.Context, key string, stdout *os.File, d time.Duration, forceFull bool) {
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: r.effects,
		Ctx:       ctx,
		Key:       key,
		Result: mvu.ActionResult{
			Delay:     d,
			ForceFull: forceFull,
		},
		Callback: func(full bool) {
			r.forceRedrawWithMode(stdout, full)
		},
	})
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

func writeAll(ctx context.Context, w io.Writer, data []byte, clk clock.Clock) error {
	if clk == nil {
		clk = clock.New()
	}
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
					case <-clk.After(5 * time.Millisecond):
					}
				} else {
					clk.Sleep(5 * time.Millisecond)
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
				case <-clk.After(5 * time.Millisecond):
				}
			} else {
				clk.Sleep(5 * time.Millisecond)
			}
		}
	}
	return nil
}

func (r *Runner) setActiveSession(id string, local bool) {
	r.scrollbackMu.Lock()
	if r.scrollbackView.Active() && r.scrollbackSession != "" && r.scrollbackSession != id {
		r.scrollbackView.Exit()
		r.scrollbackSession = ""
		r.runtime().ApplyAction(mvu.ScrollbackPercentAction{Visible: false})
	}
	r.scrollbackMu.Unlock()
	r.viewMu.Lock()
	r.activeSessionID = id
	r.activeIsLocal = local
	r.viewMu.Unlock()
	if id != "" {
		r.runtime().ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{SessionID: id}})
	}
}

func (r *Runner) activeSession() (string, bool) {
	r.viewMu.RLock()
	defer r.viewMu.RUnlock()
	return r.activeSessionID, r.activeIsLocal
}

func (r *Runner) setTabSuppressed(sessionID string, on bool) {
	r.tabSuppress.Set(sessionID, on)
	r.runtime().ApplyAction(mvu.TabSuppressedAction{Suppressed: on})
}

func (r *Runner) tabSuppressed(sessionID string) bool {
	return r.tabSuppress.Active(sessionID)
}

func (r *Runner) scrollbackActiveFor(id string) bool {
	r.scrollbackMu.Lock()
	defer r.scrollbackMu.Unlock()
	return r.scrollbackView.Active() && r.scrollbackSession == id
}

func (r *Runner) isLocalActive() bool {
	r.viewMu.RLock()
	defer r.viewMu.RUnlock()
	return r.activeIsLocal
}

func (r *Runner) updateTabs(sessions []remoteSessionInfo) {
	ui := r.runtime()
	sessions = r.mergeSessions(sessions)
	activeID, _ := r.activeSession()
	if activeID == "" {
		activeID = r.firstLocalID()
	}
	sources := mvu.SessionTabSourcesFrom(sessions)
	if !mvu.SessionIDExists(sources, activeID) {
		if localID := r.firstLocalID(); localID != "" {
			activeID = localID
			r.setActiveSession(activeID, true)
		}
	}
	var disabled map[string]bool
	if r.remoteSessions != nil {
		disabled = r.remoteSessions.DisabledSessions()
	}
	tabResult := ui.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
		Sources:  sources,
		ActiveID: activeID,
		Options: mvu.BuildSessionTabsOptions{
			LocalIDs: r.localIDSet(),
			Disabled: disabled,
			Muted:    r.localOfflineSet(),
		},
	}})
	tabs := tabResult.Tabs
	activeIdx := tabResult.Active
	if r.trace != nil {
		tabTitles := make([]string, 0, len(tabs))
		for _, tab := range tabs {
			tabTitles = append(tabTitles, tab.Title)
		}
		r.trace.Event("tabs_update", map[string]any{
			"component":   "host",
			"active_id":   activeID,
			"active_idx":  activeIdx,
			"tab_count":   len(tabs),
			"tab_titles":  tabTitles,
			"session_ids": sessionIDs(sessions),
		})
	}
}

func (r *Runner) localSession(id string) *localSession {
	r.localMu.RLock()
	defer r.localMu.RUnlock()
	return r.localSessions[id]
}

func (r *Runner) cursorQueryFunc(stdout, stdin *os.File) func(terminal.Snapshot) (row, col int, ok bool) {
	return func(raw terminal.Snapshot) (row, col int, ok bool) {
		cols, rows := termSizeAny(stdout, stdin)
		// Tests and non-interactive call sites may not provide terminal files.
		// In that case, trust the raw snapshot dimensions instead of /dev/tty.
		if (stdout == nil && stdin == nil) || cols <= 0 || rows <= 0 {
			cols = raw.Cols
			rows = raw.Rows
		}
		if raw.CursorVisible && raw.Cols == cols && raw.Rows == rows {
			row = raw.Cursor.Y + 1
			col = raw.Cursor.X + 1
			if r.trace != nil {
				r.trace.Event("cursor_query_source", map[string]any{
					"component": "host",
					"source":    "raw_full",
					"row":       row,
					"col":       col,
					"cursor_x":  raw.Cursor.X,
					"cursor_y":  raw.Cursor.Y,
					"cols":      raw.Cols,
					"rows":      raw.Rows,
				})
			}
			return row, col, true
		}
		r.stdoutMu.Lock()
		topOverlayVisible := r.renderCache.TopOverlayVisible()
		r.stdoutMu.Unlock()
		r.renderCursorMu.Lock()
		if topOverlayVisible && r.renderCursorVisible {
			row = r.renderCursorRow
			col = r.renderCursorCol
			r.renderCursorMu.Unlock()
			if r.trace != nil {
				r.trace.Event("cursor_query_source", map[string]any{
					"component": "host",
					"source":    "render_overlay",
					"row":       row,
					"col":       col,
					"cursor_x":  raw.Cursor.X,
					"cursor_y":  raw.Cursor.Y,
					"cols":      raw.Cols,
					"rows":      raw.Rows,
					"view_cols": cols,
					"view_rows": rows,
				})
			}
			return row, col, true
		}
		r.renderCursorMu.Unlock()
		snap := protocol.SnapshotToProto(raw)
		row, col, ok = r.cursorQueryPosition(snap, cols, rows)
		if r.trace != nil {
			r.trace.Event("cursor_query_source", map[string]any{
				"component": "host",
				"source":    "viewport",
				"row":       row,
				"col":       col,
				"ok":        ok,
				"cursor_x":  raw.Cursor.X,
				"cursor_y":  raw.Cursor.Y,
				"cols":      raw.Cols,
				"rows":      raw.Rows,
				"view_cols": cols,
				"view_rows": rows,
			})
		}
		return row, col, ok
	}
}

func (r *Runner) cursorQueryPosition(snap *protocolpb.Snapshot, cols, rows int) (row, col int, ok bool) {
	cursor := mvu.CursorFromSnapshot(snap, cols, rows)
	if cursor.Row <= 0 || cursor.Col <= 0 {
		return 0, 0, false
	}
	return cursor.Row, cursor.Col, true
}

func (r *Runner) activeLocalSession() *localSession {
	id, local := r.activeSession()
	if !local || id == "" {
		return nil
	}
	return r.localSession(id)
}

func (r *Runner) isLocalSession(id string) bool {
	return r.localSession(id) != nil
}

func (r *Runner) localIDSet() map[string]bool {
	r.localMu.RLock()
	defer r.localMu.RUnlock()
	if len(r.localSessions) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(r.localSessions))
	for id := range r.localSessions {
		ids[id] = true
	}
	return ids
}

func (r *Runner) localOfflineSet() map[string]bool {
	r.localMu.RLock()
	defer r.localMu.RUnlock()
	if len(r.localSessions) == 0 {
		return nil
	}
	offline := make(map[string]bool, len(r.localSessions))
	for id, session := range r.localSessions {
		if session != nil && session.Offline() {
			offline[id] = true
		}
	}
	if len(offline) == 0 {
		return nil
	}
	return offline
}

func (r *Runner) localSessionsInfo() []remoteSessionInfo {
	r.localMu.RLock()
	defer r.localMu.RUnlock()
	out := make([]remoteSessionInfo, 0, len(r.localOrder))
	for _, id := range r.localOrder {
		if session := r.localSessions[id]; session != nil {
			out = append(out, remoteSessionInfo{
				ID:           session.ID(),
				Name:         session.Name(),
				Status:       "active",
				LastActiveAt: session.LastActive(),
			})
		}
	}
	return out
}

func (r *Runner) mergeSessions(remote []remoteSessionInfo) []remoteSessionInfo {
	locals := r.localSessionsInfo()
	if len(locals) == 0 && len(remote) == 0 {
		return nil
	}
	localByID := make(map[string]remoteSessionInfo, len(locals))
	localSet := make(map[string]bool, len(locals))
	for _, local := range locals {
		localByID[local.ID] = local
		localSet[local.ID] = true
	}
	r.localMu.RLock()
	closed := make(map[string]bool, len(r.localClosed))
	for id := range r.localClosed {
		closed[id] = true
	}
	r.localMu.RUnlock()

	if len(remote) > 0 && len(locals) > 0 {
		allRemoteLocal := true
		remoteByID := make(map[string]remoteSessionInfo, len(remote))
		for _, session := range remote {
			if closed[session.ID] {
				continue
			}
			if !localSet[session.ID] {
				allRemoteLocal = false
				break
			}
			remoteByID[session.ID] = session
		}
		if allRemoteLocal {
			out := make([]remoteSessionInfo, 0, len(locals))
			for _, local := range locals {
				if closed[local.ID] {
					continue
				}
				if session, ok := remoteByID[local.ID]; ok {
					if local.Name != "" {
						session.Name = local.Name
					}
					out = append(out, session)
					continue
				}
				out = append(out, local)
			}
			return r.orderSessions(out)
		}
	}

	out := make([]remoteSessionInfo, 0, len(remote)+len(locals))
	for _, session := range remote {
		if closed[session.ID] {
			continue
		}
		if local, ok := localByID[session.ID]; ok {
			if local.Name != "" {
				session.Name = local.Name
			}
			out = append(out, session)
			delete(localByID, session.ID)
			continue
		}
		out = append(out, session)
	}
	if len(localByID) == 0 {
		return r.orderSessions(out)
	}
	for _, local := range locals {
		if _, ok := localByID[local.ID]; ok {
			out = append(out, local)
		}
	}
	return r.orderSessions(out)
}

func (r *Runner) orderSessions(sessions []remoteSessionInfo) []remoteSessionInfo {
	if len(sessions) <= 1 {
		r.setSessionOrder(sessions)
		return sessions
	}
	r.sessionOrderMu.RLock()
	prior := append([]string(nil), r.sessionOrder...)
	r.sessionOrderMu.RUnlock()
	if len(prior) == 0 {
		r.setSessionOrder(sessions)
		return sessions
	}

	byID := make(map[string]remoteSessionInfo, len(sessions))
	for _, session := range sessions {
		byID[session.ID] = session
	}
	ordered := make([]remoteSessionInfo, 0, len(sessions))
	for _, id := range prior {
		session, ok := byID[id]
		if !ok {
			continue
		}
		ordered = append(ordered, session)
		delete(byID, id)
	}
	if len(byID) > 0 {
		newSessions := make([]remoteSessionInfo, 0, len(byID))
		for _, session := range byID {
			newSessions = append(newSessions, session)
		}
		sort.Slice(newSessions, func(i, j int) bool {
			return newSessions[i].ID < newSessions[j].ID
		})
		ordered = append(ordered, newSessions...)
	}
	r.setSessionOrder(ordered)
	return ordered
}

func (r *Runner) setSessionOrder(sessions []remoteSessionInfo) {
	order := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session.ID == "" {
			continue
		}
		order = append(order, session.ID)
	}
	r.sessionOrderMu.Lock()
	r.sessionOrder = order
	r.sessionOrderMu.Unlock()
}

func (r *Runner) combinedSessions() []remoteSessionInfo {
	if r.remoteSessions == nil {
		return r.mergeSessions(nil)
	}
	return r.mergeSessions(r.remoteSessions.Sessions())
}

func (r *Runner) firstLocalID() string {
	r.localMu.RLock()
	defer r.localMu.RUnlock()
	if len(r.localOrder) == 0 {
		return ""
	}
	return r.localOrder[0]
}

func (r *Runner) startInitialLocalSession(ctx context.Context, tokenRefresher func(context.Context) (string, error), gate *netgate.Gate, stdout, stdin *os.File, debugRemoteInput bool) error {
	session, err := r.addLocalSession(ctx, r.opts.SessionID, r.sessionName, r.opts.Respawn, r.opts.Offline, tokenRefresher, gate, stdout, stdin, debugRemoteInput)
	if err != nil {
		return err
	}
	r.setActiveSession(session.ID(), true)
	r.updateTabs(nil)
	r.forceRedraw(stdout)
	return nil
}

func (r *Runner) createLocalSession(ctx context.Context, tokenRefresher func(context.Context) (string, error), gate *netgate.Gate, stdout, stdin *os.File, debugRemoteInput bool) {
	sessionID, sessionName := r.nextLocalIdentity()
	if r.logger != nil {
		r.logger.Trace("session.local.create", "session", sessionID, "name", sessionName)
	}
	session, err := r.addLocalSession(ctx, sessionID, sessionName, r.opts.Respawn, r.opts.Offline, tokenRefresher, gate, stdout, stdin, debugRemoteInput)
	if err != nil {
		r.logger.Warn("session.local.create.failed", "err", err)
		return
	}
	r.activateLocalSession(session.ID(), stdout, stdin)
}

func (r *Runner) nextLocalIdentity() (string, string) {
	r.localMu.Lock()
	defer r.localMu.Unlock()
	r.localSeq++
	index := r.localSeq
	name := r.sessionBase
	if index >= 0 {
		name = fmt.Sprintf("%s-%d", r.sessionBase, index)
	}
	id := name
	for r.localSessions[id] != nil {
		index++
		name = fmt.Sprintf("%s-%d", r.sessionBase, index)
		id = name
	}
	r.localSeq = index
	return id, name
}

func parseSessionSequenceSuffix(name string) int {
	i := strings.LastIndex(name, "-")
	if i < 0 || i+1 >= len(name) {
		return -1
	}
	seq, err := strconv.Atoi(name[i+1:])
	if err != nil || seq < 0 {
		return -1
	}
	return seq
}

func (r *Runner) addLocalSession(ctx context.Context, id, name string, respawn, offline bool, tokenRefresher func(context.Context) (string, error), gate *netgate.Gate, stdout, stdin *os.File, debugRemoteInput bool) (*localSession, error) {
	session := newLocalSession(ctx, localSessionOptions{
		ID:              id,
		Name:            name,
		Shell:           r.opts.Shell,
		Term:            r.opts.Term,
		Cols:            r.opts.Cols,
		Rows:            r.opts.Rows,
		ScrollbackLines: r.opts.ScrollbackLines,
		Respawn:         respawn,
		Offline:         offline,
		Logger:          r.logger,
		Clock:           r.clock,
		OnOutput:        r.handleLocalOutput(stdout, stdin),
		OnPTYRead:       r.opts.OnPTYRead,
		OnSnapshot:      r.opts.OnSnapshot,
		OnExit:          r.handleLocalExit(stdout, stdin),
		CursorQuery:     r.cursorQueryFunc(stdout, stdin),
		Trace:           r.trace,
		DefaultFg:       r.outerDefaultFg,
		DefaultBg:       r.outerDefaultBg,
		DefaultCursor:   r.outerDefaultCursor,
	})
	session.SetLastActive(r.clock.Now())
	session.setHolder(host.HostControlID)

	r.localMu.Lock()
	if r.localSessions[id] != nil {
		r.localMu.Unlock()
		return nil, fmt.Errorf("session %q already exists", id)
	}
	r.localSessions[id] = session
	r.localOrder = append(r.localOrder, id)
	delete(r.localClosed, id)
	r.localMu.Unlock()

	if r.opts.Publish {
		publisher := host.NewPublisher(host.PublishOptions{
			Endpoint:         r.opts.Endpoint,
			Token:            r.opts.Token,
			TokenRefresher:   tokenRefresher,
			Clock:            r.clock,
			SessionID:        session.ID(),
			SessionName:      session.Name(),
			Cols:             r.opts.Cols,
			Rows:             r.opts.Rows,
			PublishControl:   r.opts.PublishControl,
			MaxReplayScreens: r.opts.MaxReplayScreens,
			TLSDir:           r.opts.TLSDir,
			Insecure:         r.opts.Insecure,
			Logger:           r.logger,
		})
		endpointLabel := config.EndpointDisplay(r.opts.Endpoint, r.opts.HostnameOnly)
		publisher.OnStatus = func(connected bool, err error) {
			if session.Offline() {
				return
			}
			if r.opts.OnPublishStatus != nil {
				status := PublishStatus{
					SessionID: session.ID(),
					Endpoint:  endpointLabel,
					Remaining: 0,
				}
				if connected {
					status.Kind = PublishStatusConnected
					status.Message = mvu.ConnectedToMessage(endpointLabel)
				} else {
					status.Kind = PublishStatusConnectionLost
					status.Message = mvu.ConnectionLostMessage(endpointLabel)
				}
				r.opts.OnPublishStatus(status)
			}
			if gate != nil {
				if connected {
					gate.Allow()
				}
			}
			activeID, _ := r.activeSession()
			if activeID != "" && !r.isActiveLocalSession(session.ID()) {
				return
			}
			if connected {
				r.showStatus(mvu.ConnectedToMessage(endpointLabel), stdout, 3*time.Second)
				return
			}
			msg := mvu.ConnectionLostMessage(endpointLabel)
			r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
				Kind:     mvu.StatusConnectionLost,
				Message:  msg,
				Endpoint: endpointLabel,
			}})
			r.forceRedraw(stdout)
			_ = err
		}
		publisher.OnSessionRejected = func(message string) {
			r.handlePublisherSessionRejected(session, message, stdout)
		}
		publisher.OnBackoff = func(remaining time.Duration) {
			if session.Offline() {
				return
			}
			if r.opts.OnPublishStatus != nil {
				r.opts.OnPublishStatus(PublishStatus{
					SessionID: session.ID(),
					Kind:      PublishStatusConnectionBackoff,
					Message:   mvu.ConnectionLostBackoffMessage(endpointLabel, remaining),
					Endpoint:  endpointLabel,
					Remaining: remaining,
				})
			}
			if gate != nil {
				gate.BlockFor(remaining)
			}
			activeID, _ := r.activeSession()
			if activeID != "" && !r.isActiveLocalSession(session.ID()) {
				return
			}
			msg := mvu.ConnectionLostBackoffMessage(endpointLabel, remaining)
			r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
				Kind:      mvu.StatusConnectionBackoff,
				Message:   msg,
				Endpoint:  endpointLabel,
				Remaining: remaining,
			}})
			r.forceRedraw(stdout)
		}
		if r.opts.OnPublishFrame != nil {
			publisher.OnFrame = r.opts.OnPublishFrame
		}
		publisher.OnInput = func(data []byte) {
			r.handleRemoteInput(session, data, debugRemoteInput)
		}
		publisher.OnCommand = func(kind protocolpb.CommandKind) {
			r.HandleSessionCommand(session.ctx, session.ID(), kind)
		}
		publisher.OnResize = func(cols, rows int) {
			if cols <= 0 || rows <= 0 {
				return
			}
			if session.holder() == host.HostControlID {
				return
			}
			if _, err := session.Resize(cols, rows); err != nil {
				return
			}
		}
		publisher.OnControl = func(holderID string) {
			if holderID == "" {
				return
			}
			session.setHolder(holderID)
		}
		publisher.OnSessions = func(infos []*protocolpb.SessionInfo) {
			if r.remoteSessions == nil {
				return
			}
			r.remoteSessions.applySessions(toRemoteSessionsFromProto(infos))
		}
		publisher.OnWall = func(wall *protocolpb.Wall) {
			if wall == nil {
				return
			}
			if r.opts.OnPublishWall != nil {
				r.opts.OnPublishWall(wall)
			}
			if r.suppressFocusedLocalInactivityWall(wall) {
				return
			}
			r.showWall(wall, stdout)
		}
		session.SetPublisher(publisher)
		go func() {
			if err := publisher.Run(session.ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Warn("session.publisher.stop.failed", "err", err, "session", session.ID())
			}
		}()
	}

	go session.Run()
	return session, nil
}

func (r *Runner) handleLocalOutput(stdout, stdin *os.File) func(id string, data []byte, snap *protocolpb.Snapshot) {
	return func(id string, data []byte, snap *protocolpb.Snapshot) {
		if !r.isActiveLocalSession(id) {
			return
		}
		if r.scrollbackActiveFor(id) {
			return
		}
		local := r.localSession(id)
		if local == nil {
			return
		}
		r.noteLocalActivity(id)
		r.noteLocalOutput(id, data)
		if snap == nil {
			r.forceRedraw(stdout)
			return
		}
		if err := r.renderSnapshotWithOverlays(r.runCtx, stdout, stdin, snap); err != nil {
			r.logger.Debug("session.render.failed", "err", err, "session", id)
		}
	}
}

func containsEnter(data []byte) bool {
	return bytes.IndexByte(data, '\r') >= 0 || bytes.IndexByte(data, '\n') >= 0
}

func (r *Runner) noteLocalEnterInput(sessionID string, data []byte) {
	if r.trace == nil {
		return
	}
	if !containsEnter(data) {
		return
	}
	now := time.Now()
	if r.clock != nil {
		now = r.clock.Now()
	}
	r.inputTraceMu.Lock()
	if r.inputTraceEnter == nil {
		r.inputTraceEnter = make(map[string]time.Time)
	}
	if r.inputTracePending == nil {
		r.inputTracePending = make(map[string]bool)
	}
	r.inputTraceEnter[sessionID] = now
	r.inputTracePending[sessionID] = true
	r.inputTraceMu.Unlock()
	r.trace.Event("pty_input_enter", map[string]any{
		"component":  "host",
		"session_id": sessionID,
		"input":      trace.SummarizeBytes(data, 120),
	})
}

func (r *Runner) noteLocalOutput(sessionID string, data []byte) {
	if r.trace == nil {
		return
	}
	r.inputTraceMu.Lock()
	pending := r.inputTracePending[sessionID]
	start := r.inputTraceEnter[sessionID]
	if !pending {
		r.inputTraceMu.Unlock()
		return
	}
	r.inputTracePending[sessionID] = false
	r.inputTraceMu.Unlock()
	if start.IsZero() {
		return
	}
	now := time.Now()
	if r.clock != nil {
		now = r.clock.Now()
	}
	delta := now.Sub(start)
	r.trace.Event("pty_first_output_after_enter", map[string]any{
		"component":  "host",
		"session_id": sessionID,
		"delta_ms":   delta.Milliseconds(),
		"delta":      delta.String(),
		"output":     trace.SummarizeBytes(data, 120),
	})
}

func (r *Runner) addInputPrefill(data []byte) {
	if len(data) == 0 {
		return
	}
	r.inputPrefillMu.Lock()
	r.inputPrefill = append(r.inputPrefill, data...)
	r.inputPrefillMu.Unlock()
}

func (r *Runner) consumeInputPrefill(buf []byte) (int, bool) {
	r.inputPrefillMu.Lock()
	defer r.inputPrefillMu.Unlock()
	if len(r.inputPrefill) == 0 {
		return 0, false
	}
	n := copy(buf, r.inputPrefill)
	r.inputPrefill = r.inputPrefill[n:]
	return n, true
}

func (r *Runner) updateOuterDefaults(code int, payload string, source string) {
	if payload == "" || payload == "?" {
		return
	}
	r.outerOscMu.Lock()
	changed := false
	fg := r.outerDefaultFg
	bg := r.outerDefaultBg
	cursor := r.outerDefaultCursor
	switch code {
	case 10:
		if fg != payload {
			fg = payload
			changed = true
		}
	case 11:
		if bg != payload {
			bg = payload
			changed = true
		}
	case 12:
		if cursor != payload {
			cursor = payload
			changed = true
		}
	}
	r.outerDefaultFg = fg
	r.outerDefaultBg = bg
	r.outerDefaultCursor = cursor
	r.outerOscMu.Unlock()
	if !changed {
		return
	}
	r.localMu.RLock()
	for _, sess := range r.localSessions {
		sess.setOscDefaults(fg, bg, cursor)
	}
	r.localMu.RUnlock()
	if r.trace != nil {
		r.trace.Event("outer_osc_query_update", map[string]any{
			"component": "host",
			"code":      code,
			"payload":   payload,
			"source":    source,
		})
	}
}

func (r *Runner) filterOuterOSC(data []byte) []byte {
	r.outerOscMu.Lock()
	pending := r.outerOscPending
	deadline := r.outerOscDeadline
	grace := r.outerOscGraceUntil
	hadPending := pending != nil && len(pending) > 0
	if !hadPending && grace.IsZero() {
		// Keyboard input must never be delayed by OSC parsing.
		// Only filter while an explicit OSC query response is pending/in-grace.
		if r.outerOscParser.state != 0 || len(r.outerOscParser.passthrough) > 0 {
			r.outerOscParser.resetAll()
		}
		r.outerOscMu.Unlock()
		return data
	}
	now := time.Now()
	if r.clock != nil {
		now = r.clock.Now()
	}
	if !deadline.IsZero() && now.After(deadline) {
		r.outerOscPending = nil
		r.outerOscDeadline = time.Time{}
		r.outerOscGraceUntil = now.Add(oscProbeGrace)
		r.outerOscParser.resetAll()
		pending = nil
		grace = r.outerOscGraceUntil
	}
	inGrace := !grace.IsZero() && now.Before(grace)
	out := make([]byte, 0, len(data))
	for _, b := range data {
		code, payload, raw, ok := r.outerOscParser.Feed(b)
		if ok {
			if (pending != nil && pending[code]) || code == 10 || code == 11 || code == 12 {
				if pending != nil && pending[code] {
					delete(pending, code)
				}
				if payload != "" && payload != "?" {
					r.outerOscMu.Unlock()
					r.updateOuterDefaults(code, payload, "async")
					r.outerOscMu.Lock()
				}
			} else {
				r.outerOscParser.AddPassthrough(raw)
			}
		}
		if chunk := r.outerOscParser.DrainPassthrough(); len(chunk) > 0 {
			out = append(out, chunk...)
		}
	}
	if hadPending && len(pending) == 0 {
		r.outerOscPending = nil
		r.outerOscDeadline = time.Time{}
		if r.outerOscGraceUntil.IsZero() {
			r.outerOscGraceUntil = now.Add(oscProbeGrace)
		}
	}
	if inGrace && len(out) == 0 {
		r.outerOscMu.Unlock()
		return nil
	}
	if !grace.IsZero() && now.After(grace) {
		r.outerOscGraceUntil = time.Time{}
	}
	r.outerOscMu.Unlock()
	return out
}

func (r *Runner) handleLocalExit(stdout, stdin *os.File) func(id string, err error) {
	return func(id string, err error) {
		r.removeLocalSession(id, stdout, stdin)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Debug("session.local.exit", "session", id, "err", err)
		}
	}
}

func (r *Runner) removeLocalSession(id string, stdout, stdin *os.File) {
	r.localMu.Lock()
	session := r.localSessions[id]
	if session == nil {
		r.localMu.Unlock()
		return
	}
	delete(r.localSessions, id)
	r.localClosed[id] = true
	for i, existing := range r.localOrder {
		if existing == id {
			r.localOrder = append(r.localOrder[:i], r.localOrder[i+1:]...)
			break
		}
	}
	remaining := len(r.localOrder)
	r.localMu.Unlock()
	r.inputTraceMu.Lock()
	if r.inputTraceEnter != nil {
		delete(r.inputTraceEnter, id)
	}
	if r.inputTracePending != nil {
		delete(r.inputTracePending, id)
	}
	r.inputTraceMu.Unlock()
	r.setTabSuppressed(id, false)
	r.disableLocalWallNotification(id)

	activeID, activeLocal := r.activeSession()
	if activeLocal && activeID == id {
		if !r.activateAnyLocal(stdout, stdin) {
			r.requestStop()
			return
		}
	}
	if remaining == 0 {
		r.requestStop()
		return
	}
	if r.remoteSessions != nil {
		r.updateTabs(r.remoteSessions.Sessions())
	} else {
		r.updateTabs(nil)
	}
	r.forceRedraw(stdout)
	r.refreshTabBar(stdout)
}

func (r *Runner) closeLocalSession(id string, stdout, stdin *os.File) {
	local := r.localSession(id)
	if local == nil {
		return
	}
	local.Stop()
	r.removeLocalSession(id, stdout, stdin)
}

func (r *Runner) activateAnyLocal(stdout, stdin *os.File) bool {
	id := r.firstLocalID()
	if id == "" {
		return false
	}
	r.activateLocalSession(id, stdout, stdin)
	return true
}

func (r *Runner) handleSessionListUpdate(sessions []remoteSessionInfo, stdout, stdin *os.File) {
	r.updateTabs(sessions)
	activeID, _ := r.activeSession()
	merged := r.mergeSessions(sessions)
	mergedSources := mvu.SessionTabSourcesFrom(merged)
	if activeID != "" && mvu.SessionIDExists(mergedSources, activeID) {
		r.refreshTabBar(stdout)
		return
	}
	if localID := r.firstLocalID(); localID != "" {
		r.activateLocalSession(localID, stdout, stdin)
		return
	}
	r.refreshTabBar(stdout)
}

func (r *Runner) isActiveLocalSession(id string) bool {
	activeID, activeLocal := r.activeSession()
	return activeLocal && activeID == id
}

func (r *Runner) handleRemoteInput(session *localSession, data []byte, debug bool) {
	if session == nil || len(data) == 0 {
		return
	}
	holder := session.holder()
	if debug && r.logger != nil {
		r.logger.Debug("session.remote.input.received", "len", len(data), "holder", holder, "session", session.ID())
	}
	data = session.filterRemoteInput(data)
	if len(data) == 0 {
		if debug && r.logger != nil {
			r.logger.Debug("session.remote.input.ignored", "reason", "filtered", "len", len(data), "session", session.ID())
		}
		return
	}
	if _, err := session.writePTY(data); err != nil {
		if r.logger != nil {
			r.logger.Debug("session.remote.input.write.failed", "err", err, "session", session.ID())
		}
		return
	}
	if debug && r.logger != nil {
		r.logger.Debug("session.remote.input.write.ok", "len", len(data), "session", session.ID())
	}
	r.noteLocalActivity(session.ID())
}

func (r *Runner) showStatus(message string, stdout *os.File, d time.Duration) {
	if message == "" {
		return
	}
	if r.opts.OnStatus != nil {
		r.opts.OnStatus(StatusUpdate{
			SessionID: r.opts.SessionID,
			Kind:      StatusKindInfo,
			Message:   message,
			Duration:  d,
		})
	}
	effect := r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
		Kind:     mvu.StatusConnected,
		Message:  message,
		Duration: d,
	}})
	r.showStatusOverlay(stdout, d, effect.Delay, effect.ForceFull)
}

func (r *Runner) showErrorStatus(message string, stdout *os.File, d time.Duration) {
	if message == "" {
		return
	}
	if r.opts.OnStatus != nil {
		r.opts.OnStatus(StatusUpdate{
			SessionID: r.opts.SessionID,
			Kind:      StatusKindError,
			Message:   message,
			Duration:  d,
		})
	}
	effect := r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
		Kind:     mvu.StatusError,
		Message:  message,
		Duration: d,
	}})
	r.showStatusOverlay(stdout, d, effect.Delay, effect.ForceFull)
}

func (r *Runner) showStatusOverlay(stdout *os.File, d time.Duration, effectDelay time.Duration, forceFull bool) {
	r.forceRedraw(stdout)
	delay := effectDelay
	if delay == 0 {
		delay = d
	}
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: r.effects,
		Ctx:       r.runCtx,
		Key:       mvu.EffectKeyStateExpiry,
		Result: mvu.ActionResult{
			Delay:     delay,
			ForceFull: forceFull,
		},
		Callback: func(full bool) {
			r.forceRedrawWithMode(stdout, full)
		},
	})
}

func relayRejectedStatusMessage(message string) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return "session rejected by relay"
	}
	return "session rejected by relay: " + msg
}

func (r *Runner) handlePublisherSessionRejected(session *localSession, message string, stdout *os.File) {
	if session == nil {
		return
	}
	session.SetOffline(true)
	if r.remoteSessions != nil {
		r.updateTabs(r.remoteSessions.Sessions())
	} else {
		r.updateTabs(nil)
	}
	r.disableLocalWallNotification(session.ID())
	r.refreshTabBar(stdout)
	activeID, _ := r.activeSession()
	if activeID != "" && !r.isActiveLocalSession(session.ID()) {
		return
	}
	r.showErrorStatus(relayRejectedStatusMessage(message), stdout, 3*time.Second)
}

func (r *Runner) showWall(wall *protocolpb.Wall, stdout *os.File) {
	if wall == nil {
		return
	}
	if r.suppressFocusedLocalInactivityWall(wall) {
		return
	}
	ui := r.runtime()
	sender := strings.TrimSpace(wall.Sender)
	title := "Broadcast:"
	if sender != "" {
		title = fmt.Sprintf("Broadcast from %s:", sender)
	}
	message := strings.TrimSpace(wall.Message)
	timeout := 5 * time.Second
	if wall.TimeoutSeconds > 0 {
		timeout = time.Duration(wall.TimeoutSeconds) * time.Second
	}
	effect := ui.ApplyAction(mvu.WallAction{Input: mvu.WallInput{
		Visible:  true,
		Title:    title,
		Message:  message,
		Duration: timeout,
	}})
	r.forceRedrawWithMode(stdout, effect.ForceFull)
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: r.effects,
		Ctx:       r.runCtx,
		Key:       mvu.EffectKeyStateExpiry,
		Result:    effect,
		Callback: func(full bool) {
			r.forceRedrawWithMode(stdout, full)
		},
	})
}

func (r *Runner) stopLocalWallNotifications() {
	r.wallNotifyMu.Lock()
	defer r.wallNotifyMu.Unlock()
	for _, timer := range r.wallNotifyTimer {
		if timer != nil {
			timer.Stop()
		}
	}
	r.wallNotifyAfter = nil
	r.wallNotifyTimer = nil
	r.wallNotifyArmed = nil
}

func (r *Runner) configureLocalWallNotification(sessionID string, after time.Duration) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.wallNotifyMu.Lock()
	defer r.wallNotifyMu.Unlock()
	if r.wallNotifyAfter == nil {
		r.wallNotifyAfter = make(map[string]time.Duration)
	}
	if r.wallNotifyTimer == nil {
		r.wallNotifyTimer = make(map[string]*clock.Timer)
	}
	if r.wallNotifyArmed == nil {
		r.wallNotifyArmed = make(map[string]bool)
	}
	if timer := r.wallNotifyTimer[sessionID]; timer != nil {
		timer.Stop()
		delete(r.wallNotifyTimer, sessionID)
	}
	if after <= 0 {
		delete(r.wallNotifyAfter, sessionID)
		delete(r.wallNotifyArmed, sessionID)
		return
	}
	r.wallNotifyAfter[sessionID] = after
	r.wallNotifyArmed[sessionID] = true
	r.wallNotifyTimer[sessionID] = r.clock.AfterFunc(after, func() {
		r.fireLocalWallNotification(sessionID)
	})
}

func (r *Runner) disableLocalWallNotification(sessionID string) {
	r.configureLocalWallNotification(sessionID, 0)
}

func (r *Runner) noteLocalActivity(sessionID string) {
	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	local.SetLastActive(r.clock.Now())
	r.wallNotifyMu.Lock()
	defer r.wallNotifyMu.Unlock()
	after := r.wallNotifyAfter[sessionID]
	if after <= 0 {
		return
	}
	if r.wallNotifyTimer == nil {
		r.wallNotifyTimer = make(map[string]*clock.Timer)
	}
	if timer := r.wallNotifyTimer[sessionID]; timer != nil {
		timer.Stop()
	}
	if r.wallNotifyArmed == nil {
		r.wallNotifyArmed = make(map[string]bool)
	}
	r.wallNotifyArmed[sessionID] = true
	r.wallNotifyTimer[sessionID] = r.clock.AfterFunc(after, func() {
		r.fireLocalWallNotification(sessionID)
	})
}

func (r *Runner) fireLocalWallNotification(sessionID string) {
	r.wallNotifyMu.Lock()
	after := r.wallNotifyAfter[sessionID]
	armed := r.wallNotifyArmed[sessionID]
	if after <= 0 || !armed {
		r.wallNotifyMu.Unlock()
		return
	}
	r.wallNotifyArmed[sessionID] = false
	if r.wallNotifyTimer != nil {
		delete(r.wallNotifyTimer, sessionID)
	}
	r.wallNotifyMu.Unlock()

	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	label := strings.TrimSpace(local.Name())
	if label == "" {
		label = sessionID
	}
	if !r.opts.DisableDesktopNotifications {
		if r.opts.DesktopNotifier == nil {
			r.opts.DesktopNotifier = desktopnotify.New()
		}
		if r.opts.DesktopNotifier != nil {
			_ = r.opts.DesktopNotifier.Notify(r.runCtx, desktopnotify.Request{
				Title: label,
				Body:  "inactive",
			})
		}
	}
	activeID, _ := r.activeSession()
	if activeID == sessionID {
		return
	}
	r.showWall(&protocolpb.Wall{
		Message:        label + " inactive",
		TimeoutSeconds: 5,
	}, r.stdout())
}

func (r *Runner) suppressFocusedLocalInactivityWall(wall *protocolpb.Wall) bool {
	if wall == nil {
		return false
	}
	activeID, activeLocal := r.activeSession()
	if !activeLocal || activeID == "" {
		return false
	}
	message := strings.TrimSpace(wall.GetMessage())
	if !desktopnotify.IsInactivityWallMessage(message) {
		return false
	}
	label := strings.TrimSpace(strings.TrimSuffix(message, " inactive"))
	if label == "" {
		return false
	}
	if label == activeID {
		return true
	}
	active := r.localSession(activeID)
	if active == nil {
		return false
	}
	return label == strings.TrimSpace(active.Name())
}

func (r *Runner) toggleRespawn(sessionID string, stdout *os.File) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		r.showStatus("respawn toggle is local-only", stdout, 2*time.Second)
		return
	}
	enabled := local.ToggleRespawn()
	if enabled {
		r.showStatus("respawn enabled", stdout, 2*time.Second)
		return
	}
	r.showStatus("respawn disabled", stdout, 2*time.Second)
}

func (r *Runner) toggleOffline(sessionID string, stdout *os.File) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		r.showStatus("offline toggle is local-only", stdout, 2*time.Second)
		return
	}
	offline := local.ToggleOffline()
	if r.remoteSessions != nil {
		r.updateTabs(r.remoteSessions.Sessions())
	} else {
		r.updateTabs(nil)
	}
	r.refreshTabBar(stdout)
	if offline {
		r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{Kind: mvu.StatusClear}})
		r.showStatus("offline mode on", stdout, 2*time.Second)
		return
	}
	r.showStatus("offline mode off", stdout, 2*time.Second)
}

func (r *Runner) toggleWallInactivity(ctx context.Context, sessionID string, tokenRefresher func(context.Context) (string, error), stdout *os.File) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	fallbackToggle := func() bool {
		if r.opts.ToggleWallInactivityFallback == nil {
			return false
		}
		result, err := r.opts.ToggleWallInactivityFallback(ctx, sessionID)
		if err != nil {
			r.showErrorStatus("wall inactivity toggle failed", stdout, 2*time.Second)
			return true
		}
		if result.Enabled {
			r.disableLocalWallNotification(sessionID)
			status := "wall inactivity on"
			if label := strings.TrimSpace(result.InactiveAfter); label != "" {
				status = "wall inactivity " + label
			}
			r.showStatus(status, stdout, 2*time.Second)
			return true
		}
		r.disableLocalWallNotification(sessionID)
		r.showStatus("wall inactivity off", stdout, 2*time.Second)
		return true
	}
	if local := r.localSession(sessionID); local != nil && local.Offline() {
		if fallbackToggle() {
			return
		}
		r.showErrorStatus("wall inactivity requires online session", stdout, 2*time.Second)
		return
	}
	if strings.TrimSpace(r.opts.Endpoint) == "" {
		if fallbackToggle() {
			return
		}
		r.showStatus("wall inactivity requires relay endpoint", stdout, 2*time.Second)
		return
	}
	token := strings.TrimSpace(r.opts.Token)
	if token == "" && tokenRefresher != nil {
		refreshed, err := tokenRefresher(ctx)
		if err != nil {
			if fallbackToggle() {
				return
			}
			r.showErrorStatus("wall inactivity toggle failed: token refresh", stdout, 2*time.Second)
			return
		}
		token = strings.TrimSpace(refreshed)
		if token != "" {
			r.opts.Token = token
		}
	}
	if token == "" {
		if fallbackToggle() {
			return
		}
		r.showStatus("wall inactivity requires authentication", stdout, 2*time.Second)
		return
	}
	resp, err := relayclient.ToggleWallInactivity(
		ctx,
		r.opts.Endpoint,
		token,
		sessionID,
		r.opts.TLSDir,
		r.opts.Insecure,
	)
	if err != nil {
		if fallbackToggle() {
			return
		}
		r.showErrorStatus("wall inactivity toggle failed", stdout, 2*time.Second)
		return
	}
	if resp.Enabled {
		r.configureLocalWallNotification(sessionID, parseWallInactiveAfter(resp.InactiveAfter))
		status := "wall inactivity on"
		if label := strings.TrimSpace(resp.InactiveAfter); label != "" {
			status = "wall inactivity " + label
		}
		r.showStatus(status, stdout, 2*time.Second)
		return
	}
	r.disableLocalWallNotification(sessionID)
	r.showStatus("wall inactivity off", stdout, 2*time.Second)
}

func parseWallInactiveAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	after, err := time.ParseDuration(raw)
	if err != nil || after <= 0 {
		return 0
	}
	return after
}

func (r *Runner) applyTheme(name string) {
	resolved := resolveThemeName(name)
	r.themeName = resolved
	if r.remoteSessions != nil {
		r.remoteSessions.SetTheme(resolved)
	}
	r.runtime().ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Theme: theme.TUI(resolved)}})
}

func (r *Runner) cycleTheme(stdout *os.File) {
	names := theme.Names()
	if len(names) == 0 {
		return
	}
	current := r.themeName
	if current == "" {
		current = resolveThemeName(r.opts.Theme)
	}
	next := names[0]
	for i, name := range names {
		if name == current {
			next = names[(i+1)%len(names)]
			break
		}
	}
	r.applyTheme(next)
	r.showStatus(fmt.Sprintf("theme: %s", next), stdout, 2*time.Second)
	r.forceRedraw(stdout)
}

func resolveThemeName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return config.DefaultTerminalTheme
	}
	for _, candidate := range theme.Names() {
		if candidate == trimmed {
			return candidate
		}
	}
	return config.DefaultTerminalTheme
}

func (r *Runner) refreshTabBar(stdout *os.File) {
	activeID, activeLocal := r.activeSession()
	if activeLocal {
		r.forceRedraw(stdout)
		return
	}
	if r.remoteSessions != nil {
		r.remoteSessions.Render(activeID)
	}
}

func (r *Runner) switchTab(ctx context.Context, dir int, stdout, stdin *os.File) {
	sessions := r.combinedSessions()
	if r.remoteSessions != nil && !r.hasRemoteSession(sessions) {
		_ = r.remoteSessions.Refresh(ctx)
		sessions = r.combinedSessions()
	}
	if len(sessions) == 0 {
		return
	}
	activeID, _ := r.activeSession()
	nextID := mvu.NextSessionID(mvu.SessionTabSourcesFrom(sessions), activeID, dir)
	if r.trace != nil {
		r.trace.Event("tab_switch", map[string]any{
			"component":   "host",
			"current_id":  activeID,
			"next_id":     nextID,
			"dir":         dir,
			"session_ids": sessionIDs(sessions),
		})
	}
	if nextID == "" || nextID == activeID {
		if !r.isLocalActive() {
			r.refreshTabBar(stdout)
		}
		return
	}
	if r.isLocalSession(nextID) {
		r.activateLocalSession(nextID, stdout, stdin)
		return
	}
	if err := r.activateRemote(ctx, nextID, stdout, stdin); err != nil {
		r.logger.Warn("session.remote.switch.failed", "err", err, "session", nextID)
		return
	}
	r.refreshTabBar(stdout)
}

func (r *Runner) hasRemoteSession(sessions []remoteSessionInfo) bool {
	for _, session := range sessions {
		if !r.isLocalSession(session.ID) {
			return true
		}
	}
	return false
}

func sessionIDs(sessions []remoteSessionInfo) []string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	return ids
}

func (r *Runner) activateLocalSession(sessionID string, stdout, stdin *os.File) {
	if sessionID == "" {
		return
	}
	local := r.localSession(sessionID)
	if local == nil {
		return
	}
	if r.logger != nil {
		r.logger.Trace("session.local.activate", "session", sessionID)
	}
	activeID, activeLocal := r.activeSession()
	if activeLocal && activeID == sessionID {
		return
	}
	if r.remoteSessions != nil && !activeLocal && activeID != "" {
		r.remoteSessions.Hide(activeID)
	}
	r.setActiveSession(sessionID, true)
	if local.Offline() {
		r.runtime().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{Kind: mvu.StatusClear}})
	}
	local.SetLastActive(r.clock.Now())
	if !activeLocal {
		// Switching from remote to local has no compatible local render baseline.
		// Local-to-local switches keep the previous snapshot for delta rendering.
		r.renderCache.SetPrevSnapshot(nil)
	}
	if r.remoteSessions != nil {
		r.updateTabs(r.remoteSessions.Sessions())
	} else {
		r.updateTabs(nil)
	}
	r.takeControlLocal(local, stdout, stdin)
	r.forceRedraw(stdout)
	r.stdoutMu.Lock()
	topOverlayVisible := r.renderCache.TopOverlayVisible()
	r.stdoutMu.Unlock()
	if !topOverlayVisible {
		r.refreshTabBar(stdout)
	}
}

func (r *Runner) activateRemote(ctx context.Context, sessionID string, stdout, stdin *os.File) error {
	if r.remoteSessions == nil {
		return fmt.Errorf("remote sessions unavailable")
	}
	activeID, activeLocal := r.activeSession()
	if activeID != "" && !activeLocal && activeID != sessionID {
		r.remoteSessions.Hide(activeID)
	}
	if activeLocal {
		r.renderCache.SetPrevSnapshot(nil)
	}
	if r.remoteSessions.IsDisabled(sessionID) {
		r.setActiveSession(sessionID, false)
		r.updateTabs(r.remoteSessions.Sessions())
		r.remoteSessions.Enable(ctx, sessionID, stdout)
		r.remoteSessions.RenderDisabled(sessionID, stdout)
		r.refreshTabBar(stdout)
		return nil
	}
	_, err := r.remoteSessions.Show(ctx, sessionID, stdout)
	if err != nil {
		return err
	}
	r.setActiveSession(sessionID, false)
	r.updateTabs(r.remoteSessions.Sessions())
	r.remoteSessions.RenderClear(sessionID)
	cols, rows := termSizeAny(stdout, stdin)
	if cols > 0 && rows > 0 {
		_ = r.remoteSessions.SendResize(ctx, sessionID, cols, rows)
	}
	r.refreshTabBar(stdout)
	// Force an immediate redraw on tab switch so stale overlays from the previous tab
	// do not linger until the next input/frame event.
	r.remoteSessions.Render(sessionID)
	return nil
}

func (r *Runner) takeControlLocal(local *localSession, stdout, stdin *os.File) {
	if local == nil {
		return
	}
	if r.logger != nil {
		r.logger.Trace("session.local.take_control", "session", local.ID())
	}
	local.takeControl()
	cols, rows := termSizeAny(stdout, stdin)
	if cols <= 0 || rows <= 0 {
		return
	}
	curCols, curRows := local.Size()
	if cols == curCols && rows == curRows {
		return
	}
	r.opts.Cols, r.opts.Rows = cols, rows
	if snap, err := local.Resize(cols, rows); err == nil {
		if local.publisher != nil {
			local.publisher.Resize(cols, rows, snap)
		}
	} else if r.logger != nil {
		r.logger.Debug("session.local.resize.failed", "err", err, "session", local.ID(), "cols", cols, "rows", rows)
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

func restoreCursor(w io.Writer, clk clock.Clock) {
	if w == nil {
		return
	}
	_ = writeAll(context.Background(), w, []byte("\x1b[0m\x1b[?25h"), clk)
}

func enterAltScreen(w io.Writer, clk clock.Clock) bool {
	if !isTerminalWriter(w) {
		return false
	}
	_ = writeAll(context.Background(), w, []byte("\x1b[?1049h\x1b[H"), clk)
	return true
}

func exitAltScreen(w io.Writer, clk clock.Clock) {
	if w == nil {
		return
	}
	_ = writeAll(context.Background(), w, []byte("\x1b[?1049l"), clk)
}

func isTerminalWriter(w io.Writer) bool {
	outFile, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(outFile.Fd()))
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
