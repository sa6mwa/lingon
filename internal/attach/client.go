package attach

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/control"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/retryafter"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/theme"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

var (
	clientPingInterval = 2 * time.Second
	clientPingTimeout  = 2 * time.Second
)

// ErrAuthExpired indicates the client's authentication is no longer valid.
var ErrAuthExpired = errors.New("authentication expired")

// Client attaches to a remote Lingon session.
type Client struct {
	Endpoint       string
	SessionID      string
	AccessToken    string
	ShareToken     string
	RequestControl bool
	HostnameOnly   bool
	TLSDir         string
	Insecure       bool
	UnixSocket     string
	Theme          string
	ClientID       string
	// AllowOfflineToggle permits Ctrl+L o to be forwarded to a local host transport.
	AllowOfflineToggle bool
	// DisableDesktopNotifications suppresses best-effort desktop notifications for inactivity walls.
	DisableDesktopNotifications bool
	Stdin                       io.Reader
	Stdout                      io.Writer
	Stderr                      io.Writer
	TermSize                    func() (int, int)
	ResizeEvents                <-chan struct{}
	// DisableResizePropagation treats the terminal as a camera onto the remote
	// session and suppresses resize frames from local viewport changes.
	DisableResizePropagation bool
	// DisableSignalResize suppresses process-global SIGWINCH handling and relies
	// only on explicit ResizeEvents.
	DisableSignalResize bool
	// Clock controls time for timers and ping loops.
	Clock clock.Clock
	// NoHostTimeout controls how long to wait for the first snapshot before failing.
	NoHostTimeout time.Duration
	// TokenRefresher returns a fresh access token when the current one is invalid.
	TokenRefresher func(context.Context) (string, error)

	Logger          pslog.Logger
	Trace           *trace.Writer
	DesktopNotifier desktopnotify.Notifier

	holderID string

	mu               sync.RWMutex
	lastSnapshot     *protocolpb.Snapshot
	lastSeq          uint64
	needsResync      bool
	resyncRequested  bool
	forceFreshHello  bool
	renderCache      mvu.RenderCache
	scrollbackMu     sync.RWMutex
	scrollbackBuffer *mvu.ProtoScrollbackBuffer
	scrollbackView   mvu.ScrollbackViewport
	renderMu         sync.Mutex
	writeMu          sync.Mutex
	renderReqMu      sync.Mutex
	renderReqCh      chan struct{}
	renderDirty      atomic.Uint32
	lastActivity     atomic.Int64
	stdin            io.Reader
	stdout           io.Writer
	stderr           io.Writer
	stdinCloser      io.Closer
	errOnce          sync.Once
	runErr           error
	readErrMu        sync.Mutex
	readErr          error
	controlCh        chan struct{}
	ws               *websocket.Conn
	compositor       *mvu.Runtime
	runCtx           context.Context
	readyMu          sync.Mutex
	ready            bool
	renderDisabled   bool
	forceClear       bool
	tabSuppress      mvu.CursorTabSuppression
	forceTabsVisible uint32
	followInputUntil int64
	effects          *mvu.EffectScheduler
	viewOnlyMu       sync.Mutex
	viewOnly         bool
	viewOnlyMsg      string
	viewOnlyShownAt  time.Time
	themeName        string

	OnReady func()
	// OnFrame is invoked for each frame received from the server.
	OnFrame func(*protocolpb.Frame)
	// OnSessions is invoked when a sessions update frame arrives.
	OnSessions func([]SessionInfo)
	// OnSendHello is invoked when a resync hello is sent after welcome.
	OnSendHello func(error)
	// OnWall is invoked when a wall frame arrives.
	OnWall func(*protocolpb.Wall)
	// OnRoutedHeadlessStatus handles routed status walls from local headless hosts.
	OnRoutedHeadlessStatus func(*protocolpb.Wall)
	// OnOverlayStateChange is invoked when overlay state changes asynchronously.
	OnOverlayStateChange func()
}

// Close terminates the current websocket connection, if any.
func (c *Client) Close(reason string) {
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return
	}
	_ = ws.Close(websocket.StatusNormalClosure, reason)
}

// Connected reports whether the websocket is active.
func (c *Client) Connected() bool {
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	return ws != nil
}

// HasSnapshot reports whether the client has received at least one snapshot.
func (c *Client) HasSnapshot() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSnapshot != nil
}

type runOptions struct {
	input   bool
	resize  bool
	raw     bool
	restore bool
}

// Run attaches to a session and renders output.
func (c *Client) Run(ctx context.Context) error {
	return c.run(ctx, runOptions{input: true, resize: true, raw: true, restore: true})
}

// RunDetached attaches without reading stdin or handling local resize.
func (c *Client) RunDetached(ctx context.Context) error {
	return c.run(ctx, runOptions{input: false, resize: false, raw: false, restore: false})
}

func (c *Client) run(ctx context.Context, opts runOptions) error {
	if c.Logger == nil {
		c.Logger = logging.Default()
	}
	if c.DesktopNotifier == nil && !c.DisableDesktopNotifications && c.desktopNotificationsEnabled() {
		c.DesktopNotifier = desktopnotify.New()
	}
	c.clock()
	if c.Endpoint == "" && c.UnixSocket == "" {
		return fmt.Errorf("endpoint is required")
	}
	compositor := c.ensureCompositor()
	endpointLabel := c.Endpoint
	if endpointLabel == "" {
		endpointLabel = "local://headless"
	}
	compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:     c.clock(),
		SessionID: c.SessionID,
		Endpoint:  endpointLabel,
		Theme:     theme.TUI(resolveThemeName(c.Theme)),
	}})
	c.setTheme(resolveThemeName(c.Theme))
	c.runCtx = ctx
	c.effects = mvu.NewEffectScheduler(c.clock())
	defer c.effects.StopAll()
	c.startRenderLoop(ctx)
	c.resetReady()
	c.scrollbackMu.Lock()
	if c.scrollbackBuffer == nil {
		c.scrollbackBuffer = mvu.NewProtoScrollbackBuffer(config.DefaultScrollbackLines)
	} else {
		c.scrollbackBuffer.SetLimit(config.DefaultScrollbackLines)
	}
	c.scrollbackMu.Unlock()

	wsURL := ""
	httpURL := ""
	if c.UnixSocket == "" {
		var err error
		wsURL, httpURL, err = normalizeEndpoint(c.Endpoint)
		if err != nil {
			return err
		}
	}

	if c.ClientID == "" {
		c.ClientID = c.newClientID()
	}

	c.stdin = c.stdinReader()
	c.stdout = c.stdoutWriter()
	c.stderr = c.stderrWriter()
	ownsStdin := c.Stdin != nil
	if closer, ok := c.stdin.(io.Closer); ok {
		c.stdinCloser = closer
	}
	if opts.input {
		if stdinFile, ok := c.stdin.(*os.File); ok {
			stdinState, err := term.MakeRaw(int(stdinFile.Fd()))
			if err != nil {
				if term.IsTerminal(int(stdinFile.Fd())) {
					return err
				}
			} else {
				defer func() {
					_ = term.Restore(int(stdinFile.Fd()), stdinState)
				}()
			}
		}
	}
	if opts.restore {
		defer restoreCursor(c.clock(), c.stdoutWriter())
	}

	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
	}

	dialOptions := &websocket.DialOptions{HTTPClient: &http.Client{}}
	if c.UnixSocket != "" {
		transport := &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: 5 * time.Second}
				return dialer.DialContext(ctx, "unix", c.UnixSocket)
			},
		}
		dialOptions.HTTPClient.Transport = transport
	} else {
		clientTLS, err := clientTLSConfig(c.TLSDir, c.Insecure)
		if err != nil {
			return err
		}
		dialOptions.HTTPClient.Transport = &http.Transport{
			TLSClientConfig:   clientTLS,
			DisableKeepAlives: true,
		}
	}
	if transport, ok := dialOptions.HTTPClient.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}

	wsEndpoint := wsURL + "/ws/client"
	if c.UnixSocket != "" {
		wsEndpoint = "ws://unix/ws/client"
	} else if c.ShareToken == "" {
		if c.TokenRefresher != nil {
			if _, err := c.refreshToken(ctx); err != nil {
				return authExpiredError(c.Endpoint, err)
			}
		}
		if c.AccessToken == "" {
			return fmt.Errorf("access token is required")
		}
		setDialAuthHeader(dialOptions, c.AccessToken)
	} else {
		parsed, parseErr := url.Parse(wsEndpoint)
		if parseErr == nil {
			query := parsed.Query()
			query.Set("token", c.ShareToken)
			parsed.RawQuery = query.Encode()
			wsEndpoint = parsed.String()
		} else {
			wsEndpoint = wsEndpoint + "?token=" + url.QueryEscape(c.ShareToken)
		}
	}

	dialCtx := ctx
	var dialCancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		dialCtx, dialCancel = context.WithCancel(ctx)
		timer := c.clock().AfterFunc(5*time.Second, func() {
			dialCancel()
		})
		defer timer.Stop()
		defer dialCancel()
	}
	ws, err := c.dialWithRefresh(dialCtx, wsEndpoint, dialOptions)
	if err != nil {
		return err
	}
	c.ws = ws
	c.touchActivity()
	defer func() {
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
	}()
	if c.controlCh == nil {
		c.controlCh = make(chan struct{}, 1)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := c.buildHelloFrame(cols, rows)
	lastSeq := hello.GetHello().GetLastSeq()
	if c.Trace != nil {
		c.Trace.Event("hello", map[string]any{
			"client_id": c.ClientID,
			"last_seq":  lastSeq,
		})
	}
	if c.Logger != nil {
		c.Logger.Debug("attach.client.hello", "session", c.SessionID, "client", c.ClientID, "last_seq", lastSeq)
	}
	if err := c.writeFrame(ctx, ws, hello); err != nil {
		return err
	}

	if opts.raw {
		if enterAltScreen(c.clock(), c.stdoutWriter()) {
			defer exitAltScreen(c.clock(), c.stdoutWriter())
		}
		if enableMouseReporting(c.clock(), c.stdoutWriter()) {
			defer disableMouseReporting(c.clock(), c.stdoutWriter())
		}
		if stdinFile, ok := c.stdin.(*os.File); ok && term.IsTerminal(int(stdinFile.Fd())) {
			stdinState, err := term.MakeRaw(int(stdinFile.Fd()))
			if err != nil {
				return err
			}
			defer func() {
				_ = term.Restore(int(stdinFile.Fd()), stdinState)
			}()
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.pingLoop(ctx, ws, cancel)

	wsDone := make(chan struct{})
	go func() {
		defer close(wsDone)
		c.readWS(ctx, ws)
		cancel()
	}()

	inputDone := make(chan struct{})
	if opts.input {
		go func() {
			defer close(inputDone)
			c.readInput(ctx, ws)
		}()
	} else {
		close(inputDone)
	}

	if opts.resize {
		go func() {
			c.handleResize(ctx, ws)
		}()
	}

	readyTimeout := c.NoHostTimeout
	if readyTimeout == 0 {
		readyTimeout = 5 * time.Second
	}
	readyTimer := c.clock().AfterFunc(readyTimeout, func() {
		c.readyMu.Lock()
		ready := c.ready
		c.readyMu.Unlock()
		if ready {
			return
		}
		c.setError(fmt.Errorf("no host connected"))
		cancel()
	})
	defer readyTimer.Stop()

	if opts.input && c.shouldWaitForSignals() {
		waitForSignals(ctx, cancel)
	} else {
		<-ctx.Done()
	}
	if c.stdinCloser != nil && (ownsStdin || !c.shouldWaitForSignals()) {
		_ = c.stdinCloser.Close()
	}
	<-wsDone
	if opts.input && c.shouldWaitForSignals() {
		select {
		case <-inputDone:
		case <-c.clock().After(200 * time.Millisecond):
		}
	} else {
		<-inputDone
	}

	_ = httpURL
	return c.error()
}

func (c *Client) clock() clock.Clock {
	if c.Clock == nil {
		c.Clock = clock.New()
	}
	return c.Clock
}

func (c *Client) ensureCompositor() *mvu.Runtime {
	if c.compositor != nil {
		return c.compositor
	}
	ui := mvu.NewRuntime()
	ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:     c.clock(),
		SessionID: c.SessionID,
		Endpoint:  c.Endpoint,
		Theme:     theme.TUI(resolveThemeName(c.Theme)),
	}})
	c.compositor = ui
	return ui
}

func setDialAuthHeader(opts *websocket.DialOptions, token string) {
	if opts == nil {
		return
	}
	opts.HTTPHeader = http.Header{"Authorization": {"Bearer " + token}}
}

func (c *Client) dialWithRefresh(ctx context.Context, url string, opts *websocket.DialOptions) (*websocket.Conn, error) {
	for attempt := 0; attempt < 2; attempt++ {
		ws, resp, err := websocket.Dial(ctx, url, opts)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusUnauthorized {
				if attempt == 0 && c.ShareToken == "" {
					if _, refreshErr := c.refreshToken(ctx); refreshErr == nil {
						if resp.Body != nil {
							_ = resp.Body.Close()
						}
						setDialAuthHeader(opts, c.AccessToken)
						continue
					} else {
						if resp.Body != nil {
							_ = resp.Body.Close()
						}
						return nil, authExpiredError(c.Endpoint, refreshErr)
					}
				}
				msg := authErrorMessage(resp)
				if resp.Body != nil {
					_ = resp.Body.Close()
				}
				if c.ShareToken != "" {
					return nil, fmt.Errorf("share token unauthorized: %s", msg)
				}
				return nil, authExpiredError(c.Endpoint, fmt.Errorf("%s", msg))
			}
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, err
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		ws.SetReadLimit(config.DefaultWSReadLimit)
		return ws, nil
	}
	return nil, authExpiredError(c.Endpoint, fmt.Errorf("unauthorized"))
}

func (c *Client) refreshToken(ctx context.Context) (string, error) {
	if c.TokenRefresher == nil {
		return "", fmt.Errorf("token refresh unavailable")
	}
	token, err := c.TokenRefresher(ctx)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("refresh returned empty token")
	}
	c.AccessToken = token
	return token, nil
}

func authExpiredError(endpoint string, err error) error {
	msg := "authentication expired"
	if err != nil && err.Error() != "" {
		msg = err.Error()
	}
	if endpoint == "" {
		return fmt.Errorf("%w: %s", ErrAuthExpired, msg)
	}
	return fmt.Errorf("%w: %s; run `lingon login -e %s`", ErrAuthExpired, msg, endpoint)
}

func authErrorMessage(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		if resp != nil {
			return resp.Status
		}
		return "unauthorized"
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if len(data) == 0 {
		return resp.Status
	}
	type payload struct {
		Error string `json:"error"`
	}
	var out payload
	if err := json.Unmarshal(data, &out); err == nil && out.Error != "" {
		return out.Error
	}
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return resp.Status
	}
	return msg
}

func (c *Client) pingLoop(ctx context.Context, ws *websocket.Conn, cancel func()) {
	ticker := c.clock().NewTicker(clientPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if c.idleFor() < clientPingInterval {
			continue
		}
		if !c.writeMu.TryLock() {
			continue
		}
		pingCtx, pingCancel := context.WithTimeout(ctx, clientPingTimeout)
		err := ws.Ping(pingCtx)
		pingCancel()
		c.writeMu.Unlock()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}
			c.setError(err)
			cancel()
			return
		}
		c.touchActivity()
	}
}

func (c *Client) resetReady() {
	c.readyMu.Lock()
	c.ready = false
	c.readyMu.Unlock()
}

func (c *Client) markReady() {
	c.mu.RLock()
	hasSnapshot := c.lastSnapshot != nil
	c.mu.RUnlock()
	if !hasSnapshot {
		return
	}
	c.readyMu.Lock()
	if c.ready {
		c.readyMu.Unlock()
		return
	}
	c.ready = true
	cb := c.OnReady
	c.readyMu.Unlock()
	if cb != nil {
		go cb()
	}
}

// SetStdout overrides the writer used for rendering snapshots.
func (c *Client) SetStdout(w io.Writer) {
	c.renderMu.Lock()
	c.stdout = w
	c.renderMu.Unlock()
}

// SetCompositor sets the overlay compositor for rendering.
func (c *Client) SetCompositor(ui *mvu.Runtime) {
	if ui == nil {
		ui = mvu.NewRuntime()
	}
	ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:     c.clock(),
		SessionID: c.SessionID,
		Endpoint:  c.Endpoint,
		Theme:     theme.TUI(resolveThemeName(c.Theme)),
	}})
	c.compositor = ui
}

// Compositor returns the overlay compositor in use.
func (c *Client) Compositor() *mvu.Runtime {
	return c.ensureCompositor()
}

// SetTheme updates the client theme without showing a status banner.
func (c *Client) SetTheme(name string) {
	c.setTheme(name)
}

// SetRenderDisabled forces snapshot renders to use the dimmed style.
func (c *Client) SetRenderDisabled(disabled bool) {
	c.renderMu.Lock()
	c.renderDisabled = disabled
	c.renderMu.Unlock()
}

// SeedFrom copies the last known snapshot/sequence from a previous client.
func (c *Client) SeedFrom(prev *Client) {
	if prev == nil {
		return
	}
	prev.mu.RLock()
	snap := cloneSnapshot(prev.lastSnapshot)
	prev.mu.RUnlock()
	prev.renderMu.Lock()
	renderCache := prev.renderCache
	renderCache.PrevSnapshot = cloneSnapshot(renderCache.PrevSnapshot)
	prev.renderMu.Unlock()
	c.mu.Lock()
	c.lastSnapshot = snap
	c.lastSeq = prev.lastSeq
	c.needsResync = false
	c.resyncRequested = false
	c.mu.Unlock()
	c.ClientID = prev.ClientID
	c.renderMu.Lock()
	c.renderCache = renderCache
	c.renderMu.Unlock()
}

// SendInput forwards input bytes to the server.
func (c *Client) SendInput(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if c.isViewOnly() {
		c.showViewOnlyBanner(c.viewOnlyMessage())
		return nil
	}
	data = terminal.TranslateAppCursorKeys(data, c.appCursorActive())
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return fmt.Errorf("client not connected")
	}
	c.setFollowInputWindow(150 * time.Millisecond)
	if bytes.IndexByte(data, '\r') >= 0 || bytes.IndexByte(data, '\n') >= 0 {
		c.invalidateDeltaRender()
	}
	frame := &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: data}}}
	return c.writeFrame(ctx, ws, frame)
}

func (c *Client) appCursorActive() bool {
	snap := c.getSnapshot()
	if snap == nil {
		return false
	}
	return snap.GetMode()&terminal.SnapshotModeAppCursor != 0
}

// SendCommand forwards a control command to the server.
func (c *Client) SendCommand(ctx context.Context, kind protocolpb.CommandKind) error {
	if kind == protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED {
		return nil
	}
	if c.isViewOnly() {
		c.showViewOnlyBanner(c.viewOnlyMessage())
		return nil
	}
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return fmt.Errorf("client not connected")
	}
	frame := &protocolpb.Frame{
		Payload: &protocolpb.Frame_Command{Command: &protocolpb.Command{Kind: kind}},
	}
	return c.writeFrame(ctx, ws, frame)
}

// SendResize forwards a resize request to the server.
func (c *Client) SendResize(ctx context.Context, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	if c.isViewOnly() {
		return nil
	}
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return fmt.Errorf("client not connected")
	}
	frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
	return c.writeFrame(ctx, ws, frame)
}

// RenderCurrent re-renders the last snapshot to the current output.
func (c *Client) RenderCurrent() {
	c.renderCurrent()
}

// SuppressTabsUntilCursorLeavesTopRow hides the tab bar until the cursor
// reaches the top row at least once and then leaves it.
func (c *Client) SuppressTabsUntilCursorLeavesTopRow() {
	c.tabSuppress.Start()
	c.RenderCurrent()
}

// PrepareForCtrlLClear hides tabs for the upcoming clear redraw and forces the
// next snapshot render to repaint from a clean framebuffer baseline.
func (c *Client) PrepareForCtrlLClear() {
	if c.effects != nil {
		c.effects.Stop(mvu.EffectKeyTabAutoHide)
		c.effects.Stop(mvu.EffectKeyStateExpiry)
	}
	c.tabSuppress.Start()
	c.renderMu.Lock()
	c.renderCache.Reset()
	c.forceClear = true
	c.renderMu.Unlock()
	c.RenderCurrent()
}

// ForceTabsVisibleOnce forces the next active-view render passes to keep the tab
// bar visible even if the cursor is currently on row 1.
func (c *Client) ForceTabsVisibleOnce() {
	atomic.StoreUint32(&c.forceTabsVisible, 2)
}

// RenderCurrentFull re-renders the last snapshot with a full clear.
func (c *Client) RenderCurrentFull() {
	c.renderMu.Lock()
	c.renderCache.Reset()
	c.forceClear = false
	c.renderMu.Unlock()
	c.renderCurrent()
}

// RenderCurrentClear re-renders the last snapshot with a full clear.
func (c *Client) RenderCurrentClear() {
	c.renderMu.Lock()
	c.renderCache.Reset()
	c.forceClear = true
	c.renderMu.Unlock()
	c.renderCurrent()
}

func (c *Client) invalidateDeltaRender() {
	c.renderMu.Lock()
	c.renderCache.Reset()
	c.renderMu.Unlock()
}

func (c *Client) readWS(ctx context.Context, ws *websocket.Conn) {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			c.setReadErr(err)
			var closeErr websocket.CloseError
			if errors.As(err, &closeErr) && shouldReportCloseReason(closeErr.Reason) {
				c.setError(fmt.Errorf("%s", closeErr.Reason))
			}
			return
		}
		c.touchActivity()
		if c.OnFrame != nil {
			c.OnFrame(frame)
		}
		if snapshot := frame.GetSnapshot(); snapshot != nil {
			if c.Trace != nil {
				c.Trace.Event("frame_snapshot", map[string]any{
					"client_id": c.ClientID,
					"seq":       frame.Seq,
				})
			}
			if c.Logger != nil {
				c.Logger.Debug("attach.client.frame.snapshot", "session", c.SessionID, "client", c.ClientID, "seq", frame.Seq)
			}
			c.handleSnapshot(frame.Seq, snapshot)
			continue
		}
		if diff := frame.GetDiff(); diff != nil {
			accept, resync := c.acceptSeq(frame.Seq)
			if resync {
				_ = c.requestResync(ctx, ws)
			}
			if !accept {
				continue
			}
			if c.Trace != nil {
				c.Trace.Event("frame_diff", map[string]any{
					"client_id": c.ClientID,
					"seq":       frame.Seq,
				})
			}
			if c.Logger != nil {
				c.Logger.Debug("attach.client.frame.diff", "session", c.SessionID, "client", c.ClientID, "seq", frame.Seq)
			}
			if snap := c.applyDiff(diff); snap != nil {
				c.requestRenderCurrent()
			}
			continue
		}
		if scrollback := frame.GetScrollback(); scrollback != nil {
			accept, resync := c.acceptSeq(frame.Seq)
			if resync {
				_ = c.requestResync(ctx, ws)
			}
			if !accept {
				continue
			}
			c.handleScrollback(scrollback)
			c.scrollbackMu.RLock()
			scrollbackVisible := c.scrollbackView.Visible()
			c.scrollbackMu.RUnlock()
			if scrollbackVisible {
				c.requestRenderCurrent()
			}
			continue
		}
		if sessions := frame.GetSessions(); sessions != nil {
			_, resync := c.handleSessionsFrame(frame.Seq, sessions.GetSessions())
			if resync {
				_ = c.requestResync(ctx, ws)
			}
			continue
		}
		accept, resync := c.acceptSeq(frame.Seq)
		if resync {
			_ = c.requestResync(ctx, ws)
		}
		if !accept {
			continue
		}
		if welcome := frame.GetWelcome(); welcome != nil {
			c.handleControl(welcome.HolderClientId)
			c.markReady()
			err := c.sendHello(ctx, ws)
			if c.OnSendHello != nil {
				c.OnSendHello(err)
			}
			continue
		}
		if ctrl := frame.GetControl(); ctrl != nil {
			c.handleControl(ctrl.HolderClientId)
			continue
		}
		if wall := frame.GetWall(); wall != nil {
			if headless.IsRoutedStatusSender(wall.GetSender()) {
				if c.OnRoutedHeadlessStatus != nil {
					c.OnRoutedHeadlessStatus(wall)
					continue
				}
				if c.handleRoutedHeadlessStatus(wall) {
					continue
				}
			}
			if c.OnWall != nil {
				c.OnWall(wall)
			}
			c.handleWall(wall)
			continue
		}
		if status := frame.GetWallInactivityStatus(); status != nil {
			c.handleWallInactivityStatus(status)
			continue
		}
		if errMsg := frame.GetError(); errMsg != nil {
			msg := errMsg.Message
			if msg == "" {
				msg = "connection error"
			}
			if strings.Contains(strings.ToLower(msg), "control not permitted") {
				c.setViewOnly(msg)
				continue
			}
			err := fmt.Errorf("server error: %s", msg)
			if errMsg.RetryAfterSeconds > 0 {
				c.setError(&retryafter.Error{
					Err:        err,
					RetryAfter: time.Duration(errMsg.RetryAfterSeconds) * time.Second,
				})
			} else {
				c.setError(err)
			}
			return
		}
	}
}

func (c *Client) setReadErr(err error) {
	if err == nil {
		return
	}
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	if c.readErr != nil {
		return
	}
	c.readErr = err
}

func (c *Client) handleScrollback(scrollback *protocolpb.Scrollback) {
	if scrollback == nil {
		return
	}
	c.scrollbackMu.Lock()
	if c.scrollbackBuffer == nil {
		c.scrollbackBuffer = mvu.NewProtoScrollbackBuffer(config.DefaultScrollbackLines)
	}
	c.scrollbackBuffer.Apply(scrollback)
	viewRows := c.renderCache.SnapshotRows()
	if viewRows <= 0 {
		viewRows = config.DefaultTerminalRows
	}
	totalRows := c.scrollbackBuffer.Len() + c.renderCache.SnapshotRows()
	c.scrollbackView.Normalize(totalRows, viewRows, protoScrollbackContentWidthLocked(c), c.scrollbackViewCols())
	c.scrollbackMu.Unlock()
}

func (c *Client) setScrollbackActive(active bool) {
	c.scrollbackMu.Lock()
	defer c.scrollbackMu.Unlock()
	if !active {
		c.scrollbackView.SetActive(false)
		return
	}
	snap := c.snapshotOrBlank()
	viewCols, viewRows := c.terminalSize()
	if viewCols <= 0 {
		viewCols = int(snap.Cols)
	}
	if viewRows <= 0 {
		viewRows = int(snap.Rows)
	}
	totalRows := int(snap.Rows)
	scrollRows := []*protocolpb.ScrollbackRow(nil)
	if c.scrollbackBuffer != nil {
		scrollRows = c.scrollbackBuffer.Rows()
		totalRows += len(scrollRows)
	}
	contentCols := protoScrollbackContentWidth(scrollRows, snap)
	colOffset, liveOriginRow := render.ViewportOriginForSnapshot(snap, viewCols, viewRows)
	rowOffset := int(snap.Rows) - viewRows - liveOriginRow
	if rowOffset < 0 {
		rowOffset = 0
	}
	c.scrollbackView.EnterAt(totalRows, viewRows, rowOffset, contentCols, viewCols, colOffset)
}

func (c *Client) scrollbackPage(delta int, stepRows int) bool {
	c.scrollbackMu.Lock()
	defer c.scrollbackMu.Unlock()
	viewRows := c.scrollbackViewRows()
	if viewRows <= 0 {
		return false
	}
	totalRows := c.renderCache.SnapshotRows()
	if c.scrollbackBuffer != nil {
		totalRows += c.scrollbackBuffer.Len()
	}
	c.scrollbackView.Normalize(totalRows, viewRows, protoScrollbackContentWidthLocked(c), c.scrollbackViewCols())
	return c.scrollbackView.Page(totalRows, viewRows, delta, stepRows)
}

func (c *Client) scrollbackPanX(delta int) bool {
	c.scrollbackMu.Lock()
	defer c.scrollbackMu.Unlock()
	totalRows := c.renderCache.SnapshotRows()
	if c.scrollbackBuffer != nil {
		totalRows += c.scrollbackBuffer.Len()
	}
	viewRows := c.scrollbackViewRows()
	viewCols := c.scrollbackViewCols()
	contentCols := protoScrollbackContentWidthLocked(c)
	c.scrollbackView.Normalize(totalRows, viewRows, contentCols, viewCols)
	return c.scrollbackView.PanX(contentCols, viewCols, delta)
}

// ReadErr returns the first websocket read error, if any.
func (c *Client) ReadErr() error {
	c.readErrMu.Lock()
	defer c.readErrMu.Unlock()
	return c.readErr
}

func (c *Client) handleControl(holder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if holder == c.holderID {
		return
	}
	c.holderID = holder
	if c.controlCh == nil {
		return
	}
	select {
	case c.controlCh <- struct{}{}:
	default:
	}
}

func (c *Client) isController() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.holderID != "" && c.holderID == c.ClientID
}

func (c *Client) handleSnapshot(seq uint64, snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	c.mu.Lock()
	c.lastSnapshot = snap
	if seq != 0 {
		c.lastSeq = seq
	}
	c.needsResync = false
	c.resyncRequested = false
	c.forceFreshHello = false
	c.mu.Unlock()
	c.renderMu.Lock()
	c.renderCache.Reset()
	c.renderMu.Unlock()
	c.requestRenderCurrent()
	c.markReady()
}

func (c *Client) getSnapshot() *protocolpb.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSnapshot
}

// Snapshot returns a clone of the last known snapshot (if any).
func (c *Client) Snapshot() *protocolpb.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSnapshot(c.lastSnapshot)
}

func (c *Client) applyDiff(diff *protocolpb.Diff) *protocolpb.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	if diff == nil {
		return c.lastSnapshot
	}
	cols := int(diff.Cols)
	rows := int(diff.Rows)
	if cols <= 0 || rows <= 0 {
		if c.lastSnapshot == nil {
			return nil
		}
		cols = int(c.lastSnapshot.Cols)
		rows = int(c.lastSnapshot.Rows)
	}

	if c.lastSnapshot == nil || int(c.lastSnapshot.Cols) != cols || int(c.lastSnapshot.Rows) != rows {
		hasGraphemes := false
		for _, row := range diff.DiffRows {
			if len(row.Graphemes) > 0 {
				hasGraphemes = true
				break
			}
		}
		c.lastSnapshot = &protocolpb.Snapshot{
			Cols:      uint32(cols),
			Rows:      uint32(rows),
			Runes:     make([]uint32, cols*rows),
			Modes:     make([]int32, cols*rows),
			Fg:        make([]uint32, cols*rows),
			Bg:        make([]uint32, cols*rows),
			Graphemes: nil,
		}
		if hasGraphemes {
			c.lastSnapshot.Graphemes = make([]string, cols*rows)
		}
	}

	snap := c.lastSnapshot
	if len(snap.Graphemes) == 0 {
		for _, row := range diff.DiffRows {
			if len(row.Graphemes) > 0 {
				snap.Graphemes = make([]string, cols*rows)
				break
			}
		}
	}
	for _, row := range diff.DiffRows {
		y := int(row.Row)
		if y < 0 || y >= rows {
			continue
		}
		start := y * cols
		for x := 0; x < cols; x++ {
			idx := start + x
			if x < len(row.Runes) {
				snap.Runes[idx] = row.Runes[x]
			}
			if x < len(row.Modes) {
				snap.Modes[idx] = row.Modes[x]
			}
			if x < len(row.Fg) {
				snap.Fg[idx] = row.Fg[x]
			}
			if x < len(row.Bg) {
				snap.Bg[idx] = row.Bg[x]
			}
			if len(snap.Graphemes) > 0 && x < len(row.Graphemes) {
				snap.Graphemes[idx] = row.Graphemes[x]
			}
		}
	}
	if diff.Cursor != nil {
		snap.Cursor = diff.Cursor
	}
	snap.CursorVisible = diff.CursorVisible
	snap.Mode = diff.Mode
	snap.Title = diff.Title
	return snap
}

func (c *Client) renderSnapshot(snap *protocolpb.Snapshot) {
	c.renderMu.Lock()
	defer c.renderMu.Unlock()
	forceFull := c.forceClear
	c.forceClear = false
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = int(snap.Cols), int(snap.Rows)
	}
	c.scrollbackMu.Lock()
	var scrollRows []*protocolpb.ScrollbackRow
	if c.scrollbackBuffer != nil {
		scrollRows = c.scrollbackBuffer.Rows()
	}
	totalRows := len(scrollRows) + int(snap.Rows)
	contentCols := protoScrollbackContentWidth(scrollRows, snap)
	c.scrollbackView.Normalize(totalRows, rows, contentCols, cols)
	scrollOffset := c.scrollbackView.Offset()
	scrollCol := c.scrollbackView.Column()
	scrollActive := c.scrollbackView.Active()
	scrollbackVisible := c.scrollbackView.Visible()
	c.scrollbackMu.Unlock()
	viewSnap := snap
	if scrollActive || scrollOffset > 0 {
		viewSnap = mvu.BuildScrollbackViewFromProto(cols, rows, scrollRows, snap, scrollOffset, scrollCol)
	}
	compositor := c.ensureCompositor()
	if scrollbackVisible {
		percent := c.scrollbackView.Percent(len(scrollRows)+int(snap.Rows), rows)
		compositor.ApplyAction(mvu.ScrollbackPercentAction{Visible: true, Percent: percent})
	} else {
		compositor.ApplyAction(mvu.ScrollbackPercentAction{Visible: false})
	}
	if c.renderDisabled {
		c.renderDisabledSnapshot(viewSnap)
		return
	}
	now := c.clock().Now()
	if !scrollActive && !scrollbackVisible && !c.followHorizontalCursor(now) {
		viewSnap = clampSnapshotCursorToViewport(viewSnap, cols)
	}
	cursor := mvu.CursorFromSnapshot(viewSnap, cols, rows)
	row := cursor.Row
	col := cursor.Col
	visible := cursor.Visible
	suppressed := c.tabSuppress.Resolve(cursor.Row)
	forceTabsVisible := false
	for {
		n := atomic.LoadUint32(&c.forceTabsVisible)
		if n == 0 {
			break
		}
		if atomic.CompareAndSwapUint32(&c.forceTabsVisible, n, n-1) {
			forceTabsVisible = true
			break
		}
	}
	frame, err := compositor.RenderAttachFrame(mvu.RuntimeAttachFrameInput{
		Snapshot:          viewSnap,
		Cols:              cols,
		Rows:              rows,
		Cursor:            cursor,
		Now:               now,
		ForceFull:         forceFull,
		SuppressTabs:      suppressed,
		ForceTabsVisible:  forceTabsVisible,
		ScrollbackVisible: scrollbackVisible,
		Cache:             &c.renderCache,
	})
	if err != nil {
		return
	}
	rendered := frame.Rendered
	if c.Trace != nil {
		c.Trace.Event("render", map[string]any{
			"component":           "attach",
			"session_id":          c.SessionID,
			"overlay":             true,
			"help_visible":        rendered.Resolved.HelpVisible,
			"wall_visible":        rendered.Resolved.WallVisible,
			"tab_bar_visible":     rendered.Resolved.TabBarVisible,
			"top_overlay_visible": rendered.Resolved.TopOverlayVisible,
			"disconnect_visible":  rendered.Resolved.DisconnectVisible,
			"cursor_row":          row,
			"cursor_col":          col,
			"cursor_visible":      visible,
			"cols":                cols,
			"rows":                rows,
		})
	}
	_ = writeAll(c.clock(), c.stdoutWriter(), rendered.Bytes)
	c.scheduleRedrawEffect(mvu.EffectKeyTabAutoHide, frame.TabDelay, false)
	c.scheduleRedrawEffect(mvu.EffectKeyStateExpiry, frame.StateDelay, true)
}

func (c *Client) startRenderLoop(ctx context.Context) {
	c.renderReqMu.Lock()
	if c.renderReqCh != nil {
		c.renderReqMu.Unlock()
		return
	}
	ch := make(chan struct{}, 1)
	c.renderReqCh = ch
	c.renderReqMu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
			}
			for {
				c.renderDirty.Store(0)
				c.renderCurrent()
				if c.renderDirty.Load() == 0 {
					break
				}
			}
		}
	}()
}

func (c *Client) requestRenderCurrent() {
	c.renderReqMu.Lock()
	ch := c.renderReqCh
	c.renderReqMu.Unlock()
	if ch == nil {
		c.renderCurrent()
		return
	}
	c.renderDirty.Store(1)
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (c *Client) setFollowInputWindow(d time.Duration) {
	if d <= 0 {
		atomic.StoreInt64(&c.followInputUntil, 0)
		return
	}
	atomic.StoreInt64(&c.followInputUntil, c.clock().Now().Add(d).UnixNano())
}

func (c *Client) followHorizontalCursor(now time.Time) bool {
	until := atomic.LoadInt64(&c.followInputUntil)
	if until == 0 {
		return false
	}
	return now.UnixNano() <= until
}

func clampSnapshotCursorToViewport(snap *protocolpb.Snapshot, viewCols int) *protocolpb.Snapshot {
	if snap == nil || snap.Cursor == nil || viewCols <= 0 {
		return snap
	}
	maxX := viewCols - 1
	if maxX < 0 {
		maxX = 0
	}
	if int(snap.Cursor.GetX()) <= maxX {
		return snap
	}
	clone := cloneSnapshot(snap)
	if clone.Cursor == nil {
		clone.Cursor = &protocolpb.Cursor{}
	}
	clone.Cursor.X = uint32(maxX)
	return clone
}

func (c *Client) renderCurrent() {
	c.renderSnapshot(c.snapshotOrBlank())
}

// SetScrollbackActive toggles scrollback buffer mode.
func (c *Client) SetScrollbackActive(active bool) {
	c.setScrollbackActive(active)
}

// ScrollbackPage adjusts scrollback offset by a page.
func (c *Client) ScrollbackPage(delta int, viewRows int) bool {
	return c.scrollbackPage(delta, viewRows)
}

// ScrollbackPanX adjusts horizontal pan in scrollback mode.
func (c *Client) ScrollbackPanX(delta int) bool {
	return c.scrollbackPanX(delta)
}

// ScrollbackTop jumps to the start of the scrollback buffer.
func (c *Client) ScrollbackTop(viewRows int) {
	c.scrollbackMu.Lock()
	defer c.scrollbackMu.Unlock()
	totalRows := c.renderCache.SnapshotRows()
	if c.scrollbackBuffer != nil {
		totalRows += c.scrollbackBuffer.Len()
	}
	contentCols := protoScrollbackContentWidthLocked(c)
	viewCols := c.scrollbackViewCols()
	if viewRows <= 0 {
		viewRows = c.scrollbackViewRows()
	}
	c.scrollbackView.Normalize(totalRows, viewRows, contentCols, viewCols)
	c.scrollbackView.Top(totalRows, viewRows)
}

// ScrollbackBottom jumps back to the live view position.
func (c *Client) ScrollbackBottom() {
	c.scrollbackMu.Lock()
	defer c.scrollbackMu.Unlock()
	totalRows := c.renderCache.SnapshotRows()
	if c.scrollbackBuffer != nil {
		totalRows += c.scrollbackBuffer.Len()
	}
	c.scrollbackView.Normalize(totalRows, c.scrollbackViewRows(), protoScrollbackContentWidthLocked(c), c.scrollbackViewCols())
	c.scrollbackView.Bottom()
}

// ScrollbackReset exits scrollback mode and returns to live view.
func (c *Client) ScrollbackReset() {
	c.setScrollbackActive(false)
}

// ScrollbackActive reports whether the client is in scrollback mode.
func (c *Client) ScrollbackActive() bool {
	c.scrollbackMu.RLock()
	active := c.scrollbackView.Active()
	c.scrollbackMu.RUnlock()
	return active
}

// ScrollbackOffset reports the current scrollback offset.
func (c *Client) ScrollbackOffset() int {
	c.scrollbackMu.RLock()
	defer c.scrollbackMu.RUnlock()
	return c.scrollbackView.Offset()
}

func (c *Client) scrollbackViewCols() int {
	cols, _ := c.terminalSize()
	if cols > 0 {
		return cols
	}
	if c.lastSnapshot != nil && c.lastSnapshot.Cols > 0 {
		return int(c.lastSnapshot.Cols)
	}
	return config.DefaultTerminalCols
}

func (c *Client) scrollbackViewRows() int {
	_, rows := c.terminalSize()
	if rows > 0 {
		return rows
	}
	if c.renderCache.SnapshotRows() > 0 {
		return c.renderCache.SnapshotRows()
	}
	return config.DefaultTerminalRows
}

func protoScrollbackContentWidth(scrollRows []*protocolpb.ScrollbackRow, snap *protocolpb.Snapshot) int {
	width := 0
	if snap != nil {
		width = int(snap.Cols)
	}
	for _, row := range scrollRows {
		if row == nil {
			continue
		}
		for _, candidate := range []int{len(row.Runes), len(row.Modes), len(row.Fg), len(row.Bg), len(row.Graphemes)} {
			if candidate > width {
				width = candidate
			}
		}
	}
	return width
}

func protoScrollbackContentWidthLocked(c *Client) int {
	var rows []*protocolpb.ScrollbackRow
	if c.scrollbackBuffer != nil {
		rows = c.scrollbackBuffer.Rows()
	}
	return protoScrollbackContentWidth(rows, c.lastSnapshot)
}

// RenderDisabled redraws the terminal in a dimmed style for deactivated views.
func (c *Client) RenderDisabled() {
	c.renderMu.Lock()
	defer c.renderMu.Unlock()
	c.renderDisabledSnapshot(c.snapshotOrBlank())
}

func (c *Client) renderDisabledSnapshot(snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	compositor := c.ensureCompositor()
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = int(snap.Cols), int(snap.Rows)
	}
	cursor := mvu.CursorFromSnapshot(snap, cols, rows)
	cursor.Visible = false
	frame, err := compositor.RenderDisabledFrame(mvu.RuntimeDisabledFrameInput{
		Snapshot:          snap,
		Cols:              cols,
		Rows:              rows,
		Cursor:            cursor,
		Now:               c.clock().Now(),
		ScrollbackVisible: c.ScrollbackActive(),
		Cache:             &c.renderCache,
	})
	if err != nil {
		return
	}
	rendered := frame.Rendered
	_ = writeAll(c.clock(), c.stdoutWriter(), rendered.Bytes)
	c.scheduleRedrawEffect(mvu.EffectKeyTabAutoHide, frame.TabDelay, false)
}

func (c *Client) snapshotOrBlank() *protocolpb.Snapshot {
	if snap := c.Snapshot(); snap != nil {
		return snap
	}
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
	}
	return mvu.BlankSnapshot(cols, rows)
}

func cloneSnapshot(snap *protocolpb.Snapshot) *protocolpb.Snapshot {
	if snap == nil {
		return nil
	}
	clone := proto.Clone(snap)
	if out, ok := clone.(*protocolpb.Snapshot); ok {
		return out
	}
	return snap
}

func (c *Client) scheduleRedrawEffect(key string, d time.Duration, notifyOverlayChange bool) {
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: c.effects,
		Ctx:       c.runCtx,
		Key:       key,
		Result: mvu.ActionResult{
			Delay: d,
		},
		Callback: func(_ bool) {
			if notifyOverlayChange {
				if cb := c.OnOverlayStateChange; cb != nil {
					cb()
				}
			}
			c.RenderCurrent()
		},
	})
}

func (c *Client) acceptSeq(seq uint64) (bool, bool) {
	if seq == 0 {
		return true, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSeq == 0 {
		c.lastSeq = seq
		return true, false
	}
	if seq == c.lastSeq+1 {
		c.lastSeq = seq
		return true, false
	}
	if seq <= c.lastSeq {
		return false, false
	}
	if c.Logger != nil {
		c.Logger.Debug("attach.client.seq_gap", "session", c.SessionID, "client", c.ClientID, "last_seq", c.lastSeq, "next_seq", seq)
	}
	c.lastSeq = seq
	c.needsResync = true
	c.resyncRequested = false
	return true, true
}

func (c *Client) requestResync(ctx context.Context, ws *websocket.Conn) error {
	c.mu.Lock()
	if !c.needsResync || c.resyncRequested {
		c.mu.Unlock()
		return nil
	}
	c.resyncRequested = true
	c.forceFreshHello = true
	c.mu.Unlock()
	if c.Logger != nil {
		c.Logger.Debug("attach.client.resync", "session", c.SessionID, "client", c.ClientID)
	}
	return c.sendHello(ctx, ws)
}

func (c *Client) handleSessionsFrame(seq uint64, sessions []*protocolpb.SessionInfo) (bool, bool) {
	accept, resync := c.acceptSeq(seq)
	if !accept {
		return false, resync
	}
	if c.OnSessions != nil {
		c.OnSessions(decodeSessionInfos(sessions))
	}
	return true, resync
}

func (c *Client) setError(err error) {
	if err == nil {
		return
	}
	c.errOnce.Do(func() {
		c.runErr = err
	})
}

func (c *Client) setViewOnly(message string) {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "control not permitted"
	}
	c.viewOnlyMu.Lock()
	c.viewOnly = true
	c.viewOnlyMsg = msg
	c.viewOnlyShownAt = c.clock().Now()
	c.viewOnlyMu.Unlock()
	c.showViewOnlyBanner(msg)
}

func (c *Client) isViewOnly() bool {
	c.viewOnlyMu.Lock()
	defer c.viewOnlyMu.Unlock()
	return c.viewOnly
}

func (c *Client) viewOnlyMessage() string {
	c.viewOnlyMu.Lock()
	defer c.viewOnlyMu.Unlock()
	if c.viewOnlyMsg == "" {
		return "control not permitted"
	}
	return c.viewOnlyMsg
}

func (c *Client) showViewOnlyBanner(message string) {
	if message == "" {
		return
	}
	c.showStatusBanner(mvu.StatusInput{
		Kind:     mvu.StatusError,
		Message:  message,
		Duration: 3 * time.Second,
	})
}

func wallTimeout(wall *protocolpb.Wall) time.Duration {
	if wall == nil || wall.TimeoutSeconds == 0 {
		return 5 * time.Second
	}
	return time.Duration(wall.TimeoutSeconds) * time.Second
}

func (c *Client) handleWall(wall *protocolpb.Wall) {
	if wall == nil {
		return
	}
	c.notifyDesktop(wall)
	compositor := c.ensureCompositor()
	sender := desktopnotify.FormatWallSource(wall)
	title := "Broadcast:"
	if sender != "" {
		title = fmt.Sprintf("Broadcast from %s:", sender)
	}
	timeout := wallTimeout(wall)
	effect := compositor.ApplyAction(mvu.WallAction{Input: mvu.WallInput{
		Visible:  true,
		Title:    title,
		Message:  strings.TrimSpace(wall.Message),
		Duration: timeout,
	}})
	c.RenderCurrent()
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: c.effects,
		Ctx:       c.runCtx,
		Key:       mvu.EffectKeyStateExpiry,
		Result:    effect,
		Callback: func(_ bool) {
			if cb := c.OnOverlayStateChange; cb != nil {
				cb()
			}
			c.RenderCurrent()
		},
	})
}

func (c *Client) desktopNotificationsEnabled() bool {
	if c.UnixSocket != "" {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(c.Endpoint), "local://")
}

func (c *Client) ensureDesktopNotifier() desktopnotify.Notifier {
	if c.DisableDesktopNotifications || !c.desktopNotificationsEnabled() {
		return nil
	}
	if c.DesktopNotifier == nil {
		c.DesktopNotifier = desktopnotify.New()
	}
	return c.DesktopNotifier
}

func (c *Client) notifyDesktop(wall *protocolpb.Wall) {
	if wall == nil {
		return
	}
	if !desktopnotify.IsInactivityWall(wall) {
		return
	}
	notifier := c.ensureDesktopNotifier()
	if notifier == nil {
		return
	}
	label := strings.TrimSpace(wall.GetSourceSessionId())
	if label == "" {
		label = "Lingon"
	}
	_ = notifier.Notify(c.runCtx, desktopnotify.Request{
		Title: label,
		Body:  "inactive",
	})
}

func (c *Client) handleRoutedHeadlessStatus(wall *protocolpb.Wall) bool {
	if wall == nil || !headless.IsRoutedStatusSender(wall.GetSender()) {
		return false
	}
	message := strings.TrimSpace(wall.GetMessage())
	if message == "" {
		return true
	}
	input := mvu.StatusInput{
		Endpoint: c.Endpoint,
		Message:  message,
	}
	switch wall.GetSender() {
	case headless.RoutedStatusSenderConnected:
		input.Kind = mvu.StatusConnected
		timeout := wallTimeout(wall)
		if timeout <= 0 {
			timeout = 3 * time.Second
		}
		input.Duration = timeout
	case headless.RoutedStatusSenderLost:
		input.Kind = mvu.StatusConnectionLost
	case headless.RoutedStatusSenderBackoff:
		input.Kind = mvu.StatusConnectionBackoff
		if wall.TimeoutSeconds > 0 {
			input.Remaining = time.Duration(wall.TimeoutSeconds) * time.Second
		}
	case headless.RoutedStatusSenderInfo:
		input.Kind = mvu.StatusConnected
		timeout := wallTimeout(wall)
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		input.Duration = timeout
	case headless.RoutedStatusSenderError:
		input.Kind = mvu.StatusError
		timeout := wallTimeout(wall)
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		input.Duration = timeout
	default:
		return false
	}
	c.showStatusBanner(input)
	return true
}

func wallInactivityStatusMessage(status *protocolpb.WallInactivityStatus) (message string, kind mvu.StatusKind) {
	if status == nil {
		return "", mvu.StatusConnected
	}
	if errText := strings.TrimSpace(status.GetError()); errText != "" {
		return errText, mvu.StatusError
	}
	if status.GetEnabled() {
		if label := strings.TrimSpace(status.GetInactiveAfter()); label != "" {
			return "wall inactivity " + label, mvu.StatusConnected
		}
		return "wall inactivity on", mvu.StatusConnected
	}
	return "wall inactivity off", mvu.StatusConnected
}

func (c *Client) handleWallInactivityStatus(status *protocolpb.WallInactivityStatus) {
	message, kind := wallInactivityStatusMessage(status)
	if message == "" {
		return
	}
	c.showStatusBanner(mvu.StatusInput{
		Kind:     kind,
		Message:  message,
		Duration: 2 * time.Second,
	})
}

func (c *Client) toggleWallInactivity(ctx context.Context) {
	if strings.TrimSpace(c.ShareToken) != "" {
		c.showViewOnlyBanner("wall inactivity toggle unavailable for share sessions")
		return
	}
	if strings.TrimSpace(c.Endpoint) == "" || strings.TrimSpace(c.SessionID) == "" {
		c.showViewOnlyBanner("wall inactivity toggle unavailable")
		return
	}
	token := strings.TrimSpace(c.AccessToken)
	if token == "" && c.TokenRefresher != nil {
		refreshed, err := c.TokenRefresher(ctx)
		if err != nil {
			c.showViewOnlyBanner("wall inactivity toggle failed: token refresh")
			return
		}
		token = strings.TrimSpace(refreshed)
		if token != "" {
			c.AccessToken = token
		}
	}
	if token == "" {
		c.showViewOnlyBanner("wall inactivity toggle requires authentication")
		return
	}
	resp, err := relayclient.ToggleWallInactivity(
		ctx,
		c.Endpoint,
		token,
		c.SessionID,
		c.TLSDir,
		c.Insecure,
	)
	if err != nil {
		c.showViewOnlyBanner("wall inactivity toggle failed")
		return
	}
	c.handleWallInactivityStatus(&protocolpb.WallInactivityStatus{
		Enabled:       resp.Enabled,
		InactiveAfter: strings.TrimSpace(resp.InactiveAfter),
	})
}

func (c *Client) setTheme(name string) {
	resolved := resolveThemeName(name)
	c.themeName = resolved
	c.ensureCompositor().ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Theme: theme.TUI(resolved)}})
}

func (c *Client) cycleTheme() {
	next := nextThemeName(c.themeName)
	c.setTheme(next)
	c.showThemeStatus(next)
	c.RenderCurrentClear()
}

func (c *Client) showThemeStatus(name string) {
	c.showInfoStatus(fmt.Sprintf("theme: %s", name))
}

func (c *Client) showInfoStatus(message string) {
	if message == "" {
		return
	}
	c.showStatusBanner(mvu.StatusInput{
		Kind:     mvu.StatusConnected,
		Message:  message,
		Duration: 2 * time.Second,
	})
}

func (c *Client) showStatusBanner(input mvu.StatusInput) {
	if strings.TrimSpace(input.Message) == "" {
		return
	}
	effect := c.ensureCompositor().ApplyAction(mvu.StatusAction{Input: input})
	c.RenderCurrent()
	mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
		Scheduler: c.effects,
		Ctx:       c.runCtx,
		Key:       mvu.EffectKeyStateExpiry,
		Result:    effect,
		Callback: func(_ bool) {
			if cb := c.OnOverlayStateChange; cb != nil {
				cb()
			}
			c.RenderCurrent()
		},
	})
}

func (c *Client) helpVisible() bool {
	if c.ensureCompositor().Read().HelpVisible {
		return true
	}
	c.renderMu.Lock()
	visible := c.renderCache.HelpVisible()
	c.renderMu.Unlock()
	return visible
}

func (c *Client) error() error {
	return c.runErr
}

func (c *Client) helloLastSeq() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.forceFreshHello {
		return 0
	}
	return c.lastSeq
}

func (c *Client) buildHelloFrame(cols, rows int) *protocolpb.Frame {
	lastSeq := c.helloLastSeq()
	return &protocolpb.Frame{
		SessionId: c.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     c.ClientID,
			Cols:         uint32(cols),
			Rows:         uint32(rows),
			WantsControl: c.RequestControl,
			LastSeq:      lastSeq,
			ClientType:   "attach",
		}},
	}
}

func (c *Client) sendHello(ctx context.Context, ws *websocket.Conn) error {
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
	}
	frame := c.buildHelloFrame(cols, rows)
	return c.writeFrame(ctx, ws, frame)
}

func (c *Client) newClientID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("client-%d", c.clock().Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func (c *Client) writeFrame(ctx context.Context, ws *websocket.Conn, frame *protocolpb.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := writeFrame(ctx, ws, frame); err != nil {
		return err
	}
	c.touchActivity()
	return nil
}

func (c *Client) touchActivity() {
	if c == nil {
		return
	}
	now := c.clock().Now()
	c.lastActivity.Store(now.UnixNano())
}

func (c *Client) idleFor() time.Duration {
	if c == nil {
		return 0
	}
	nanos := c.lastActivity.Load()
	if nanos <= 0 {
		return 0
	}
	last := time.Unix(0, nanos)
	now := c.clock().Now()
	if now.After(last) {
		return now.Sub(last)
	}
	return 0
}

func (c *Client) readInput(ctx context.Context, ws *websocket.Conn) {
	reader := bufio.NewReader(c.stdinReader())
	buf := make([]byte, 1024)
	prefill := make([]byte, 0, 1024)
	var prefix control.Prefix
	pending := make([]byte, 0, 2048)
	var scrollState scrollInputState
	var mouseFilter mouseReportFilter
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n := 0
		if len(prefill) > 0 {
			n = copy(buf, prefill)
			prefill = prefill[n:]
		} else {
			var err error
			n, err = reader.Read(buf)
			if err != nil {
				if !isBenignStdinReadErr(err) {
					c.Logger.Debug("attach.stdin.read.failed", "err", err)
				}
				return
			}
		}
		if n == 0 {
			continue
		}
		data := buf[:n]
		pending = pending[:0]
		flushPending := func() bool {
			if len(pending) == 0 {
				return true
			}
			if err := c.SendInput(ctx, pending); err != nil {
				c.Logger.Debug("attach.ws.write.failed", "err", err)
				c.setError(err)
				return false
			}
			pending = pending[:0]
			return true
		}
		processNormalByte := func(b byte) bool {
			helpVisible := c.helpVisible()
			if action, ok := mvu.ActionForHelpDismissKey(helpVisible, b); ok {
				c.ensureCompositor().ApplyAction(action)
				c.renderCurrent()
				return true
			}
			// Help modal is input-modal: consume all non-dismiss keys.
			if helpVisible {
				return true
			}
			if b == 0x04 {
				if !flushPending() {
					return false
				}
				_ = ws.Close(websocket.StatusNormalClosure, "ctrl-d")
				return false
			}
			action, out := prefix.Feed(b)
			if action != control.ActionNone {
				if !flushPending() {
					return false
				}
				switch action {
				case control.ActionHelp:
					c.ensureCompositor().ApplyAction(mvu.HelpVisibleAction{Visible: true})
					c.renderCurrent()
					return true
				case control.ActionToggleTabBar:
					c.ensureCompositor().ApplyAction(mvu.TabToggleAction{})
					c.renderCurrent()
					return true
				}
				if uiAction, ok := mvu.ActionForControl(action); ok {
					c.ensureCompositor().ApplyAction(uiAction)
					c.renderCurrent()
					return true
				}
				switch action {
				case control.ActionQuit:
					_ = ws.Close(websocket.StatusNormalClosure, "detached")
					return false
				case control.ActionSendCtrlD:
					if err := c.SendCommand(ctx, protocolpb.CommandKind_COMMAND_KIND_SEND_EOF); err != nil {
						c.Logger.Debug("attach.ws.write.failed", "err", err)
						c.setError(err)
						return false
					}
				case control.ActionScrollback:
					c.setScrollbackActive(true)
					c.renderCurrent()
				case control.ActionToggleWallInactivity:
					if c.AllowOfflineToggle {
						if err := c.SendCommand(ctx, protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY); err != nil {
							c.Logger.Debug("attach.ws.write.failed", "err", err)
							c.setError(err)
							return false
						}
					} else {
						c.toggleWallInactivity(ctx)
					}
				case control.ActionToggleRespawn:
					if c.AllowOfflineToggle {
						if err := c.SendCommand(ctx, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_RESPAWN); err != nil {
							c.Logger.Debug("attach.ws.write.failed", "err", err)
							c.setError(err)
							return false
						}
					} else {
						c.showInfoStatus("respawn toggle is host local-only")
					}
				case control.ActionToggleOffline:
					if c.AllowOfflineToggle {
						if err := c.SendCommand(ctx, protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE); err != nil {
							c.Logger.Debug("attach.ws.write.failed", "err", err)
							c.setError(err)
							return false
						}
					} else {
						c.showInfoStatus("offline toggle is host local-only")
					}
				case control.ActionNextTheme:
					c.cycleTheme()
				}
				return true
			}
			if len(out) > 0 {
				if c.isViewOnly() {
					c.showViewOnlyBanner(c.viewOnlyMessage())
					pending = pending[:0]
					return true
				}
				if len(out) == 1 && out[0] == 0x0c {
					c.PrepareForCtrlLClear()
				}
				pending = append(pending, out...)
				if inputEndsLineAttach(out) {
					if !flushPending() {
						return false
					}
				}
			}
			return true
		}
		filtered := make([]byte, 0, 8)
		for _, b := range data {
			if c.ScrollbackActive() {
				cmd := scrollState.feed(b)
				if cmd == scrollExit {
					c.setScrollbackActive(false)
					c.renderCurrent()
					continue
				}
				if cmd != scrollNone {
					rows := c.scrollbackViewRows()
					half := rows / 2
					if half < 1 {
						half = 1
					}
					changed := false
					switch cmd {
					case scrollPageUp:
						changed = c.scrollbackPage(1, half)
					case scrollPageDown:
						changed = c.scrollbackPage(-1, half)
					case scrollLineUp:
						changed = c.scrollbackPage(1, 1)
					case scrollLineDown:
						changed = c.scrollbackPage(-1, 1)
					case scrollFiveUp:
						changed = c.scrollbackPage(1, 5)
					case scrollFiveDown:
						changed = c.scrollbackPage(-1, 5)
					case scrollLeft:
						changed = c.scrollbackPanX(-1)
					case scrollRight:
						changed = c.scrollbackPanX(1)
					case scrollFarLeft:
						changed = c.scrollbackPanX(-5)
					case scrollFarRight:
						changed = c.scrollbackPanX(5)
					case scrollTop:
						c.ScrollbackTop(rows)
						changed = true
					case scrollBottom:
						c.ScrollbackBottom()
						changed = true
					case scrollWheelUp:
						changed = c.scrollbackPage(1, 3)
					case scrollWheelDown:
						changed = c.scrollbackPage(-1, 3)
					}
					if changed {
						c.renderCurrent()
					}
				}
				continue
			}
			filtered = filterMouseByte(&mouseFilter, b, filtered)
			for _, fb := range filtered {
				if !processNormalByte(fb) {
					return
				}
			}
			filtered = filtered[:0]
		}
		if !flushPending() {
			return
		}
	}
}

func inputEndsLineAttach(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	last := data[len(data)-1]
	return last == '\r' || last == '\n'
}

func writeAll(clk clock.Clock, w io.Writer, data []byte) error {
	if clk == nil {
		clk = clock.New()
	}
	for len(data) > 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			clk.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}

func restoreCursor(clk clock.Clock, w io.Writer) {
	if w == nil {
		return
	}
	_ = writeAll(clk, w, []byte("\x1b[0m\x1b[?25h"))
}

func enterAltScreen(clk clock.Clock, w io.Writer) bool {
	if !isTerminalWriter(w) {
		return false
	}
	_ = writeAll(clk, w, []byte("\x1b[?1049h\x1b[H"))
	return true
}

func enableMouseReporting(clk clock.Clock, w io.Writer) bool {
	if !isTerminalWriter(w) {
		return false
	}
	_ = writeAll(clk, w, []byte("\x1b[?1000h\x1b[?1006h"))
	return true
}

func exitAltScreen(clk clock.Clock, w io.Writer) {
	if w == nil {
		return
	}
	_ = writeAll(clk, w, []byte("\x1b[?1049l"))
}

func disableMouseReporting(clk clock.Clock, w io.Writer) {
	if w == nil {
		return
	}
	_ = writeAll(clk, w, []byte("\x1b[?1006l\x1b[?1000l"))
}

func isTerminalWriter(w io.Writer) bool {
	outFile, ok := w.(*os.File)
	if !ok || outFile == nil {
		return false
	}
	return term.IsTerminal(int(outFile.Fd()))
}

func normalizeEndpoint(endpoint string) (string, string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", fmt.Errorf("endpoint must include scheme")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme == "" {
		return "", "", fmt.Errorf("endpoint must include scheme")
	}
	wsURL := *parsed
	httpURL := *parsed

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		wsURL.Scheme = "wss"
	case "http":
		wsURL.Scheme = "ws"
	case "wss":
		httpURL.Scheme = "https"
	case "ws":
		httpURL.Scheme = "http"
	default:
		return "", "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	return wsURL.String(), httpURL.String(), nil
}

func clientTLSConfig(tlsDir string, insecure bool) (*tls.Config, error) {
	dir := strings.TrimSpace(tlsDir)
	if dir == "" {
		dir = config.DefaultTLSDir()
	}
	pool, err := tlsmgr.LoadLocalCARoots(dir, nil)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	}, nil
}

func readFrame(ctx context.Context, conn *websocket.Conn) (*protocolpb.Frame, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame *protocolpb.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

func decodeSessionInfos(infos []*protocolpb.SessionInfo) []SessionInfo {
	if len(infos) == 0 {
		return nil
	}
	out := make([]SessionInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		out = append(out, SessionInfo{
			ID:           info.Id,
			Name:         info.Name,
			Status:       info.Status,
			LastActiveAt: time.Unix(info.LastActiveUnix, 0).UTC(),
		})
	}
	return out
}

func shouldReportCloseReason(reason string) bool {
	if reason == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "closing", "bye", "send complete", "timeout", "flap", "disabled", "reconnect", "switch", "ctrl-d", "ctrl-l-q", "detached":
		return false
	default:
		return true
	}
}

func (c *Client) handleResize(ctx context.Context, ws *websocket.Conn) {
	ch, stop := subscribeResizeSignals(c.DisableSignalResize)
	defer stop()
	resizeEvents := c.ResizeEvents

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			cols, rows := c.terminalSize()
			c.RenderCurrentFull()
			if !c.DisableResizePropagation && c.isController() {
				frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
				_ = c.writeFrame(ctx, ws, frame)
			}
		case <-resizeEvents:
			cols, rows := c.terminalSize()
			c.RenderCurrentFull()
			if !c.DisableResizePropagation && c.isController() {
				frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
				_ = c.writeFrame(ctx, ws, frame)
			}
		case <-c.controlCh:
			if c.DisableResizePropagation || !c.isController() || ws == nil {
				continue
			}
			cols, rows := c.terminalSize()
			frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
			_ = c.writeFrame(ctx, ws, frame)
		}
	}
}

func waitForSignals(ctx context.Context, cancel func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(ch)
	select {
	case <-ctx.Done():
		return
	case <-ch:
		cancel()
	}
}

func (c *Client) stdinReader() io.Reader {
	if c.stdin != nil {
		return c.stdin
	}
	if c.Stdin != nil {
		return c.Stdin
	}
	return os.Stdin
}

func (c *Client) stdoutWriter() io.Writer {
	if c.stdout != nil {
		return c.stdout
	}
	if c.Stdout != nil {
		return c.Stdout
	}
	return os.Stdout
}

func (c *Client) stderrWriter() io.Writer {
	if c.stderr != nil {
		return c.stderr
	}
	if c.Stderr != nil {
		return c.Stderr
	}
	return os.Stderr
}

func (c *Client) terminalSize() (int, int) {
	if c.TermSize != nil {
		return c.TermSize()
	}
	if outFile, ok := c.stdoutWriter().(*os.File); ok && term.IsTerminal(int(outFile.Fd())) {
		cols, rows, err := term.GetSize(int(outFile.Fd()))
		if err == nil {
			return cols, rows
		}
	}
	return 0, 0
}

func (c *Client) shouldWaitForSignals() bool {
	if inFile, ok := c.stdinReader().(*os.File); ok && term.IsTerminal(int(inFile.Fd())) {
		return true
	}
	return false
}
