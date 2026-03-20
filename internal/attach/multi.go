package attach

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/control"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/retryafter"
	"pkt.systems/lingon/internal/theme"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

// MultiClient manages multiple attach sessions with tab switching.
type MultiClient struct {
	Endpoint       string
	AccessToken    string
	RequestControl bool
	HostnameOnly   bool
	SessionID      string
	TLSDir         string
	Insecure       bool
	// SessionSource lists sessions for tab discovery/refresh.
	// When nil, sessions are fetched from relay /sessions.
	SessionSource func(context.Context) ([]SessionInfo, error)
	// SocketResolver maps a session id to a unix domain socket path.
	// Used for local transports (for example headless local PTY mode).
	SocketResolver func(sessionID string) (string, error)
	// AllowOfflineToggle forwards Ctrl+L o to local transports.
	AllowOfflineToggle bool
	// SessionEvents triggers immediate session-list refreshes.
	// Intended for local transports to avoid long polling intervals.
	SessionEvents <-chan struct{}
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	TermSize      func() (int, int)
	Logger        pslog.Logger
	Theme         string
	// AuthFile is the path to the auth state file used for refresh.
	AuthFile string
	// TokenRefresher returns a fresh access token when the current one is invalid.
	TokenRefresher func(context.Context) (string, error)
	// Clock controls time for reconnects and countdowns.
	Clock clock.Clock
	// Trace captures structured debug events.
	Trace *trace.Writer
	// OnView is called when a session view is created.
	OnView func(sessionID string, client *Client)
	// OnReconnect is called before attempting to reconnect a view.
	OnReconnect func(sessionID string, attempt int)
	// OnViewClosed is called when a view disconnects, before reconnect logic.
	OnViewClosed func(sessionID string, visible bool, current bool)
	// OnActive is called after a session view becomes active.
	OnActive func(sessionID string)
	// BackoffPolicy overrides the default reconnect backoff.
	BackoffPolicy backoff.Policy
	// Gate throttles session list/stream/network retries.
	Gate *netgate.Gate

	InactiveTTL     time.Duration
	RefreshInterval time.Duration

	tokenMu       sync.Mutex
	fatalMu       sync.Mutex
	fatalErr      error
	backoffPolicy backoff.Policy
	cancel        context.CancelFunc
	stdinCloser   io.Closer
}

var errNoSessions = errors.New("no sessions available")

func isTerminalHostError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no host connected") || strings.Contains(msg, "host disconnected")
}

func normalizeReconnectDelay(delay, policyBase time.Duration) time.Duration {
	if delay > 0 {
		return delay
	}
	if policyBase > 0 {
		return policyBase
	}
	return backoff.DefaultPolicy.Base
}

type sessionView struct {
	id       string
	name     string
	client   *Client
	cancel   context.CancelFunc
	done     chan error
	visible  bool
	hiddenAt time.Time
	readyAt  time.Time
	removed  bool

	connecting    bool
	connected     bool
	flushingInput bool
	connectedOnce bool
	reconnectAt   time.Time
	reconnectGen  uint64
	pendingOps    []pendingOp
}

type pendingOp struct {
	input   []byte
	command protocolpb.CommandKind
}

func selectRenderableView(views map[string]*sessionView, activeID string) (*sessionView, string) {
	view := views[activeID]
	if view == nil || !view.visible {
		for _, candidate := range views {
			if candidate != nil && candidate.visible {
				view = candidate
				break
			}
		}
	}
	if view == nil {
		for _, candidate := range views {
			if candidate != nil {
				view = candidate
				break
			}
		}
	}
	if view == nil {
		return nil, activeID
	}
	return view, view.id
}

// Run starts the multi-session attach client.
func (m *MultiClient) Run(ctx context.Context) error {
	if m.Logger == nil {
		m.Logger = logging.Default()
	}
	if m.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	localSessionMode := m.SessionSource != nil
	if m.Clock == nil {
		m.Clock = clock.New()
	}
	if m.Gate == nil {
		m.Gate = netgate.New(m.Clock)
	}
	if m.InactiveTTL == 0 {
		m.InactiveTTL = 60 * time.Second
	}
	if m.RefreshInterval == 0 {
		if !localSessionMode {
			m.RefreshInterval = 60 * time.Second
		}
	}
	if !localSessionMode && m.TokenRefresher == nil && m.AuthFile != "" {
		m.TokenRefresher = relayclient.TokenRefresher(m.Endpoint, m.AuthFile, m.TLSDir, m.Insecure, func(token string) {
			m.AccessToken = token
		})
	}
	if !localSessionMode && m.AccessToken == "" && m.TokenRefresher != nil {
		token, err := m.refreshToken(ctx)
		if err != nil {
			return authExpiredError(m.Endpoint, err)
		}
		m.AccessToken = token
	}
	if !localSessionMode && m.AccessToken == "" {
		return fmt.Errorf("access token is required")
	}

	httpURL := ""
	if !localSessionMode {
		_, normalizedHTTPURL, err := normalizeEndpoint(m.Endpoint)
		if err != nil {
			return err
		}
		httpURL = normalizedHTTPURL
	}

	ui := mvu.NewRuntime()
	effects := mvu.NewEffectScheduler(m.Clock)
	defer effects.StopAll()
	endpointLabel := config.EndpointDisplay(m.Endpoint, m.HostnameOnly)
	themeName := resolveThemeName(m.Theme)
	ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:    m.Clock,
		Endpoint: m.Endpoint,
		Theme:    theme.TUI(themeName),
	}})
	stdin := m.stdinReader()
	stdout := m.stdoutWriter()
	termSize := m.TermSize
	ownsStdin := m.Stdin != nil
	defer restoreCursor(m.Clock, stdout)
	if enterAltScreen(m.Clock, stdout) {
		defer exitAltScreen(m.Clock, stdout)
	}
	if enableMouseReporting(m.Clock, stdout) {
		defer disableMouseReporting(m.Clock, stdout)
	}

	if closer, ok := stdin.(io.Closer); ok {
		m.stdinCloser = closer
		if ownsStdin {
			defer func() {
				_ = m.stdinCloser.Close()
			}()
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.cancel = cancel
	m.backoffPolicy = m.BackoffPolicy
	if m.backoffPolicy.Base == 0 {
		m.backoffPolicy = backoff.DefaultPolicy
	}

	sessions, err := m.fetchSessions(ctx, httpURL)
	if err != nil {
		return err
	}
	activeID := m.pickActiveSession(sessions)
	if activeID == "" {
		return fmt.Errorf("no sessions available")
	}

	var mu sync.Mutex
	var refreshMu sync.Mutex
	var lastRefresh time.Time
	var refreshSessions func() int
	var forceRefreshSessions func() int
	var applySessions func([]SessionInfo)
	var activateView func(string) error
	views := make(map[string]*sessionView)
	backoffAttempts := make(map[string]int)
	removedSessions := make(map[string]struct{})

	var offlineMu sync.Mutex
	offline := false
	setOffline := func(v bool) bool {
		offlineMu.Lock()
		changed := offline != v
		offline = v
		offlineMu.Unlock()
		return changed
	}
	isOffline := func() bool {
		offlineMu.Lock()
		current := offline
		offlineMu.Unlock()
		return current
	}
	activeViewSnapshot := func() (*sessionView, *Client, bool, bool, time.Time) {
		mu.Lock()
		defer mu.Unlock()
		view, resolvedID := selectRenderableView(views, activeID)
		if view == nil {
			return nil, nil, false, false, time.Time{}
		}
		activeID = resolvedID
		connected := view.connected
		connectedOnce := view.connectedOnce
		if !connectedOnce {
			for _, candidate := range views {
				if candidate != nil && candidate.connectedOnce {
					connectedOnce = true
					break
				}
			}
		}
		if client := view.client; client != nil {
			if client.ReadErr() != nil || !client.Connected() {
				connected = false
			}
		}
		if isOffline() {
			connected = false
		}
		return view, view.client, connected, connectedOnce, view.reconnectAt
	}
	renderActiveCurrent := func() {
		_, client, _, _, _ := activeViewSnapshot()
		if client != nil && client.HasSnapshot() {
			client.RenderCurrent()
		}
	}
	const waitForSessionsGrace = 30 * time.Second
	var waitMu sync.Mutex
	waitCtrl := mvu.NewAttachWaitController(waitForSessionsGrace)
	var updateDisconnectOverlay func()
	showWaitOverlay := func() {
		waitMu.Lock()
		until := waitCtrl.WaitUntil()
		active := waitCtrl.Waiting()
		waitMu.Unlock()
		if !active {
			return
		}
		result := ui.ApplyAction(mvu.AttachConnectivityAction{Input: mvu.AttachConnectivityInput{
			Connected:          false,
			ConnectedOnce:      true,
			WaitingForSessions: true,
			WaitUntil:          until,
			Endpoint:           endpointLabel,
			Now:                m.Clock.Now(),
		}})
		if result.Changed {
			renderActiveCurrent()
		}
	}
	stopWaitForSessions := func() {
		waitMu.Lock()
		wasWaiting := waitCtrl.Stop()
		waitMu.Unlock()
		if wasWaiting && updateDisconnectOverlay != nil {
			updateDisconnectOverlay()
		}
	}
	startWaitForSessions := func() {
		waitMu.Lock()
		started := waitCtrl.Start(m.Clock.Now())
		waitMu.Unlock()
		if !started {
			return
		}
		showWaitOverlay()
		go func() {
			ticker := m.Clock.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				waitMu.Lock()
				active := waitCtrl.Waiting()
				expired := waitCtrl.Expired(m.Clock.Now())
				if expired {
					waitCtrl.Stop()
				}
				waitMu.Unlock()
				if !active {
					return
				}
				if expired {
					break
				}
				showWaitOverlay()
			}
			m.setFatal(errNoSessions)
			cancel()
		}()
	}
	isWaitingForSessions := func() bool {
		waitMu.Lock()
		active := waitCtrl.Waiting()
		waitMu.Unlock()
		return active
	}
	nextReconnectGen := func(view *sessionView) uint64 {
		if view == nil {
			return 0
		}
		mu.Lock()
		view.reconnectGen++
		gen := view.reconnectGen
		mu.Unlock()
		return gen
	}
	isReconnectActive := func(view *sessionView, gen uint64) bool {
		if view == nil {
			return false
		}
		mu.Lock()
		current := views[view.id]
		active := current == view && view.reconnectGen == gen
		mu.Unlock()
		return active
	}

	setTabs := func() {
		mu.Lock()
		active := activeID
		current := make([]SessionInfo, len(sessions))
		copy(current, sessions)
		mu.Unlock()
		muted := map[string]bool{}
		for _, session := range current {
			if session.Offline || strings.EqualFold(strings.TrimSpace(session.Status), "offline") {
				muted[session.ID] = true
			}
		}
		sources := mvu.SessionTabSourcesFrom(current)
		ui.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
			Endpoint: m.Endpoint,
			Sources:  sources,
			ActiveID: active,
			Options: mvu.BuildSessionTabsOptions{
				Muted: muted,
			},
		}})
	}
	renderTabs := func() {
		_, client, _, _, _ := activeViewSnapshot()
		if client == nil || !client.HasSnapshot() {
			return
		}
		if !localSessionMode {
			if snap := client.Snapshot(); snap != nil {
				cols, rows := client.terminalSize()
				if cols <= 0 || rows <= 0 {
					cols, rows = int(snap.Cols), int(snap.Rows)
				}
				cursor := mvu.CursorFromSnapshot(snap, cols, rows)
				if cursor.Row <= 1 {
					client.SuppressTabsUntilCursorLeavesTopRow()
					return
				}
			}
		}
		ui.ApplyAction(mvu.TabWakeAction{Duration: 0})
		client.ForceTabsVisibleOnce()
		client.RenderCurrent()
	}

	scheduleOverlayRedraw := func(result mvu.ActionResult) {
		mvu.ScheduleActionEffect(mvu.ActionEffectPlan{
			Scheduler: effects,
			Ctx:       ctx,
			Key:       mvu.EffectKeyStateExpiry,
			Result:    result,
			Callback: func(_ bool) {
				renderActiveCurrent()
			},
		})
	}
	type routedHeadlessStatus struct {
		Input     mvu.StatusInput
		ExpiresAt time.Time
		Sender    string
	}
	var routedStatusMu sync.Mutex
	routedStatuses := make(map[string]routedHeadlessStatus)
	applyConnectionStatus := func(input mvu.StatusInput) {
		effect := ui.ApplyAction(mvu.StatusAction{Input: input})
		renderActiveCurrent()
		scheduleOverlayRedraw(effect)
	}
	clearConnectionStatus := func() {
		effect := ui.ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{Kind: mvu.StatusClear}})
		renderActiveCurrent()
		scheduleOverlayRedraw(effect)
	}
	activeSessionID := func() string {
		mu.Lock()
		defer mu.Unlock()
		return activeID
	}
	activeSessionOffline := func(id string) bool {
		if strings.TrimSpace(id) == "" {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		for _, session := range sessions {
			if session.ID != id {
				continue
			}
			if session.Offline || strings.EqualFold(strings.TrimSpace(session.Status), "offline") {
				return true
			}
			return false
		}
		return false
	}
	toggleActiveSessionOffline := func() bool {
		mu.Lock()
		defer mu.Unlock()
		id := strings.TrimSpace(activeID)
		if id == "" {
			return false
		}
		for i := range sessions {
			if sessions[i].ID != id {
				continue
			}
			sessions[i].Offline = !sessions[i].Offline
			if sessions[i].Offline {
				sessions[i].Status = "offline"
			} else if strings.EqualFold(strings.TrimSpace(sessions[i].Status), "offline") {
				sessions[i].Status = "running"
			}
			return true
		}
		return false
	}
	clearActiveRoutedStatus := func() {
		current := activeSessionID()
		if current == "" {
			return
		}
		routedStatusMu.Lock()
		delete(routedStatuses, current)
		routedStatusMu.Unlock()
		clearConnectionStatus()
	}
	syncActiveRoutedStatus := func() {
		current := activeSessionID()
		if current == "" {
			return
		}
		isOffline := activeSessionOffline(current)
		now := m.Clock.Now()
		routedStatusMu.Lock()
		status, ok := routedStatuses[current]
		if ok && !status.ExpiresAt.IsZero() && !now.Before(status.ExpiresAt) {
			delete(routedStatuses, current)
			ok = false
		}
		routedStatusMu.Unlock()
		if !ok {
			clearConnectionStatus()
			return
		}
		if isOffline {
			switch status.Sender {
			case headless.RoutedStatusSenderConnected, headless.RoutedStatusSenderLost, headless.RoutedStatusSenderBackoff:
				clearConnectionStatus()
				return
			}
		}
		applyConnectionStatus(status.Input)
	}
	handleRoutedStatus := func(sessionID string, wall *protocolpb.Wall) {
		if wall == nil || !headless.IsRoutedStatusSender(wall.GetSender()) {
			return
		}
		message := strings.TrimSpace(wall.GetMessage())
		if message == "" {
			return
		}
		input := mvu.StatusInput{
			Endpoint: m.Endpoint,
			Message:  message,
		}
		expiresAt := time.Time{}
		switch wall.GetSender() {
		case headless.RoutedStatusSenderConnected:
			input.Kind = mvu.StatusConnected
			timeout := wallTimeout(wall)
			if timeout <= 0 {
				timeout = 3 * time.Second
			}
			input.Duration = timeout
			expiresAt = m.Clock.Now().Add(timeout)
		case headless.RoutedStatusSenderLost:
			input.Kind = mvu.StatusConnectionLost
		case headless.RoutedStatusSenderBackoff:
			input.Kind = mvu.StatusConnectionBackoff
			if wall.GetTimeoutSeconds() > 0 {
				input.Remaining = time.Duration(wall.GetTimeoutSeconds()) * time.Second
			}
		case headless.RoutedStatusSenderInfo:
			input.Kind = mvu.StatusConnected
			timeout := wallTimeout(wall)
			if timeout <= 0 {
				timeout = 2 * time.Second
			}
			input.Duration = timeout
			expiresAt = m.Clock.Now().Add(timeout)
		case headless.RoutedStatusSenderError:
			input.Kind = mvu.StatusError
			timeout := wallTimeout(wall)
			if timeout <= 0 {
				timeout = 2 * time.Second
			}
			input.Duration = timeout
			expiresAt = m.Clock.Now().Add(timeout)
		default:
			return
		}

		routedStatusMu.Lock()
		routedStatuses[sessionID] = routedHeadlessStatus{
			Input:     input,
			ExpiresAt: expiresAt,
			Sender:    strings.TrimSpace(wall.GetSender()),
		}
		routedStatusMu.Unlock()
		// Resolve through active-session state every time so inactive-session
		// updates cannot transiently overwrite the visible banner.
		syncActiveRoutedStatus()
	}
	updateDisconnectOverlay = func() {
		if localSessionMode {
			return
		}
		waitMu.Lock()
		waitingActive := waitCtrl.Waiting()
		until := waitCtrl.WaitUntil()
		waitMu.Unlock()
		view, _, connected, connectedOnce, reconnectAt := activeViewSnapshot()
		if view == nil {
			return
		}
		result := ui.ApplyAction(mvu.AttachConnectivityAction{Input: mvu.AttachConnectivityInput{
			Connected:          connected,
			ConnectedOnce:      connectedOnce,
			ReconnectAt:        reconnectAt,
			WaitingForSessions: waitingActive,
			WaitUntil:          until,
			Endpoint:           endpointLabel,
			Now:                m.Clock.Now(),
		}})
		if result.Changed || (!connected && connectedOnce) {
			renderActiveCurrent()
		}
	}
	showConnected := func(msg string, d time.Duration) {
		effect := ui.ApplyAction(mvu.AttachStatusAction{Input: mvu.AttachStatusInput{
			Kind:      mvu.StatusConnected,
			Connected: true,
			Endpoint:  endpointLabel,
			Message:   msg,
			Duration:  d,
			Now:       m.Clock.Now(),
		}})
		renderActiveCurrent()
		scheduleOverlayRedraw(effect)
	}
	showError := func(msg string, d time.Duration) {
		effect := ui.ApplyAction(mvu.AttachStatusAction{Input: mvu.AttachStatusInput{
			Kind:      mvu.StatusError,
			Connected: true,
			Endpoint:  endpointLabel,
			Message:   msg,
			Duration:  d,
			Now:       m.Clock.Now(),
		}})
		renderActiveCurrent()
		scheduleOverlayRedraw(effect)
	}
	setTheme := func(name string, show bool) {
		themeName = resolveThemeName(name)
		ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Theme: theme.TUI(themeName)}})
		mu.Lock()
		for _, view := range views {
			if view.client != nil {
				view.client.setTheme(themeName)
			}
		}
		mu.Unlock()
		if show {
			showConnected(fmt.Sprintf("theme: %s", themeName), 2*time.Second)
		}
		renderActiveCurrent()
	}

	var connectView func(SessionInfo, bool, *sessionView) (*sessionView, error)
	connectView = func(session SessionInfo, visible bool, prev *sessionView) (*sessionView, error) {
		if m.Logger != nil {
			m.Logger.Debug("attach.view.connect.start", "session", session.ID, "visible", visible, "has_prev", prev != nil)
		}
		if !localSessionMode && m.TokenRefresher != nil {
			if _, err := m.refreshToken(ctx); err != nil {
				return nil, authExpiredError(m.Endpoint, err)
			}
		}
		unixSocket := ""
		if m.SocketResolver != nil {
			resolvedSocket, err := m.SocketResolver(session.ID)
			if err != nil {
				return nil, err
			}
			unixSocket = strings.TrimSpace(resolvedSocket)
			if unixSocket == "" {
				return nil, fmt.Errorf("session %q has no local socket", session.ID)
			}
		}
		view := &sessionView{
			id:         session.ID,
			name:       mvu.SessionLabel(session.ID, session.Name),
			visible:    visible,
			connecting: true,
		}
		if prev != nil {
			view.connectedOnce = prev.connectedOnce
			view.reconnectAt = prev.reconnectAt
			view.reconnectGen = prev.reconnectGen
		}
		cctx, ccancel := context.WithCancel(ctx)
		var tokenRefresher func(context.Context) (string, error)
		if m.TokenRefresher != nil {
			tokenRefresher = m.refreshToken
		}
		client := &Client{
			Endpoint:           m.Endpoint,
			SessionID:          session.ID,
			UnixSocket:         unixSocket,
			AccessToken:        m.AccessToken,
			RequestControl:     visible && m.RequestControl,
			AllowOfflineToggle: m.AllowOfflineToggle,
			HostnameOnly:       m.HostnameOnly,
			TLSDir:             m.TLSDir,
			Insecure:           m.Insecure,
			Theme:              themeName,
			Stdin:              io.NopCloser(strings.NewReader("")),
			TermSize:           termSize,
			Logger:             m.Logger,
			TokenRefresher:     tokenRefresher,
			Clock:              m.Clock,
			Trace:              m.Trace,
		}
		if localSessionMode {
			client.OnRoutedHeadlessStatus = func(wall *protocolpb.Wall) {
				handleRoutedStatus(session.ID, wall)
			}
		}
		if prev != nil && prev.client != nil {
			client.SeedFrom(prev.client)
		}
		client.OnReady = func() {
			readyAt := m.Clock.Now()
			if m.Logger != nil {
				m.Logger.Debug("attach.view.ready", "session", session.ID)
			}
			showStatus := false
			var pending []pendingOp
			mu.Lock()
			view.connected = true
			view.connecting = false
			view.connectedOnce = true
			view.reconnectAt = time.Time{}
			view.reconnectGen++
			view.readyAt = readyAt
			view.flushingInput = true
			if activeID == session.ID {
				view.visible = true
				view.hiddenAt = time.Time{}
			}
			showStatus = view.visible || activeID == session.ID
			if len(view.pendingOps) > 0 {
				pending = append(pending, view.pendingOps...)
				view.pendingOps = nil
			}
			mu.Unlock()
			for {
				for _, op := range pending {
					if len(op.input) > 0 {
						if err := client.SendInput(ctx, op.input); err != nil {
							if m.Logger != nil {
								m.Logger.Debug("attach.stdin.send.flush.failed", "session", session.ID, "err", err)
							}
						}
						continue
					}
					if op.command == protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED {
						continue
					}
					if err := client.SendCommand(ctx, op.command); err != nil {
						if m.Logger != nil {
							m.Logger.Debug("attach.command.send.flush.failed", "session", session.ID, "kind", op.command.String(), "err", err)
						}
					}
				}
				pending = nil
				mu.Lock()
				if len(view.pendingOps) == 0 {
					view.flushingInput = false
					mu.Unlock()
					break
				}
				pending = append(pending, view.pendingOps...)
				view.pendingOps = nil
				mu.Unlock()
			}
			m.Clock.AfterFunc(2*time.Second, func() {
				mu.Lock()
				current := views[session.ID]
				if current != view || !view.connected || !view.readyAt.Equal(readyAt) {
					mu.Unlock()
					return
				}
				backoffAttempts[session.ID] = 0
				mu.Unlock()
			})
			if setOffline(false) {
				setTabs()
			}
			if showStatus {
				cols, rows := client.terminalSize()
				if cols == 0 || rows == 0 {
					cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
				}
				if localSessionMode || client.isController() {
					_ = client.SendResize(ctx, cols, rows)
				}
			}
			if showStatus && !localSessionMode {
				showConnected(mvu.ConnectedToMessage(endpointLabel), 3*time.Second)
			}
			if showStatus && m.OnActive != nil {
				m.OnActive(session.ID)
			}
		}
		client.OnSessions = func(updated []SessionInfo) {
			if applySessions == nil {
				return
			}
			if len(updated) > 0 {
				mvu.SortSessionsByLastActive(updated)
			}
			applySessions(updated)
		}
		client.compositor = ui
		if visible {
			client.Stdout = stdout
			client.SetStdout(stdout)
		} else {
			client.Stdout = io.Discard
			client.SetStdout(io.Discard)
		}
		view.client = client
		if m.OnView != nil {
			m.OnView(session.ID, client)
		}
		view.cancel = ccancel
		view.done = make(chan error, 1)
		go func() {
			view.done <- client.RunDetached(cctx)
		}()
		go func(current SessionInfo, v *sessionView) {
			err := <-v.done
			if v.cancel != nil {
				// Ensure per-view resources are released after RunDetached exits.
				v.cancel()
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil && !isTerminalHostError(err) {
				if setOffline(true) {
					setTabs()
				}
			}
			if errors.Is(err, ErrAuthExpired) {
				m.setFatal(err)
				return
			}
			if err != nil && !isTerminalHostError(err) {
				waitMu.Lock()
				waitCtrl.AllowStart()
				waitMu.Unlock()
			}
			if m.Logger != nil {
				m.Logger.Debug("attach.view.closed", "session", current.ID, "err", err)
			}
			mu.Lock()
			preVisible := v.visible
			preRemoved := v.removed
			_, preRemovedBySession := removedSessions[current.ID]
			preLatest := views[current.ID]
			mu.Unlock()
			if preRemoved || preRemovedBySession || preLatest != v {
				if m.OnViewClosed != nil {
					m.OnViewClosed(current.ID, preVisible, preLatest == v)
				} else if m.Logger != nil {
					m.Logger.Debug("attach.view.closed.callback.missing", "session", current.ID)
				}
				if m.Logger != nil {
					reason := "replaced"
					if preRemoved {
						reason = "removed"
					}
					if preRemovedBySession {
						reason = "removed_session"
					}
					m.Logger.Debug("attach.view.reconnect.skip", "session", current.ID, "reason", reason)
				}
				return
			}
			if m.Logger != nil {
				m.Logger.Debug("attach.view.reconnect.start", "session", current.ID)
			}
			refreshed := false
			shouldRefresh := !isOffline()
			if err == nil && refreshSessions != nil && shouldRefresh {
				count := refreshSessions()
				if m.Logger != nil {
					m.Logger.Debug("attach.sessions.refresh.after_close", "session", current.ID, "count", count, "err", err)
				}
				refreshed = true
				if ctx.Err() != nil {
					return
				}
				if count == 0 && !isWaitingForSessions() {
					return
				}
				mu.Lock()
				_, ok := views[current.ID]
				mu.Unlock()
				if !ok {
					return
				}
			}
			if err != nil && isTerminalHostError(err) && refreshSessions != nil && shouldRefresh {
				count := refreshSessions()
				if m.Logger != nil {
					m.Logger.Debug("attach.sessions.refresh.host_error", "session", current.ID, "count", count, "err", err)
				}
				if count < 0 && forceRefreshSessions != nil {
					count = forceRefreshSessions()
					if m.Logger != nil {
						m.Logger.Debug("attach.sessions.refresh.host_error.force", "session", current.ID, "count", count)
					}
				}
				refreshed = true
				if ctx.Err() != nil {
					return
				}
				if count == 0 && !isWaitingForSessions() {
					return
				}
				mu.Lock()
				_, ok := views[current.ID]
				mu.Unlock()
				if !ok {
					return
				}
			}
			if err != nil && refreshSessions != nil && !refreshed && shouldRefresh {
				count := refreshSessions()
				if m.Logger != nil {
					m.Logger.Debug("attach.sessions.refresh.fallback", "session", current.ID, "count", count, "err", err)
				}
				if count < 0 && forceRefreshSessions != nil {
					count = forceRefreshSessions()
					if m.Logger != nil {
						m.Logger.Debug("attach.sessions.refresh.fallback.force", "session", current.ID, "count", count)
					}
				}
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				_, ok := views[current.ID]
				mu.Unlock()
				if !ok {
					return
				}
			}
			currentView := v
			mu.Lock()
			currentView.connected = false
			currentView.connecting = false
			currentView.flushingInput = false
			if view := views[current.ID]; view == currentView {
				view.reconnectAt = time.Time{}
			}
			currentView.readyAt = time.Time{}
			visible := currentView.visible
			hiddenAt := currentView.hiddenAt
			removed := currentView.removed
			_, removedBySession := removedSessions[current.ID]
			attempt := backoffAttempts[current.ID]
			mu.Unlock()
			if !localSessionMode && currentView.connectedOnce && !isTerminalHostError(err) {
				result := ui.ApplyAction(mvu.AttachConnectivityAction{Input: mvu.AttachConnectivityInput{
					Connected:     false,
					ConnectedOnce: true,
					ReconnectAt:   currentView.reconnectAt,
					Endpoint:      endpointLabel,
					Now:           m.Clock.Now(),
				}})
				if result.Changed {
					renderActiveCurrent()
				}
			}
			v = currentView
			if m.OnViewClosed != nil {
				m.OnViewClosed(current.ID, visible, currentView == v)
			} else if m.Logger != nil {
				m.Logger.Debug("attach.view.closed.callback.missing", "session", current.ID)
			}
			inactiveExpired := !visible && !hiddenAt.IsZero() && m.InactiveTTL > 0 && m.Clock.Now().Sub(hiddenAt) >= m.InactiveTTL
			if removed || removedBySession || inactiveExpired {
				if m.Logger != nil {
					reason := "removed"
					if inactiveExpired {
						reason = "inactive_timeout"
					}
					if removedBySession {
						reason = "removed_session"
					}
					m.Logger.Debug("attach.view.reconnect.skip", "session", current.ID, "reason", reason)
				}
				return
			}
			gen := nextReconnectGen(currentView)
			if isReconnectActive(currentView, gen) && currentView.connectedOnce {
				updateDisconnectOverlay()
			}
			for {
				delay := m.backoffPolicy.Next(attempt)
				if retryDelay, ok := retryafter.FromError(err); ok && retryDelay > delay {
					delay = retryDelay
				}
				delay = normalizeReconnectDelay(delay, m.backoffPolicy.Base)
				attempt++
				mu.Lock()
				backoffAttempts[current.ID] = attempt
				mu.Unlock()
				if m.Logger != nil {
					m.Logger.Trace("attach.view.reconnect.schedule", "session", current.ID, "attempt", attempt, "delay", delay)
				}
				if m.Gate != nil {
					m.Gate.BlockFor(delay)
				}
				if delay > 0 {
					deadline := m.Clock.Now().Add(delay)
					mu.Lock()
					if view := views[current.ID]; view == currentView {
						view.reconnectAt = deadline
					}
					mu.Unlock()
					if isReconnectActive(currentView, gen) && currentView.connectedOnce {
						updateDisconnectOverlay()
					}
					ticker := m.Clock.NewTicker(time.Second)
					streamWasOffline := isOffline()
					interrupted := false
					for {
						if streamWasOffline && !isOffline() {
							interrupted = true
							break
						}
						remaining := int(deadline.Sub(m.Clock.Now()).Seconds())
						if remaining <= 0 {
							break
						}
						select {
						case <-ctx.Done():
							ticker.Stop()
							return
						case <-ticker.C:
							if isReconnectActive(currentView, gen) && currentView.connectedOnce {
								updateDisconnectOverlay()
							}
						}
					}
					ticker.Stop()
					if interrupted {
						mu.Lock()
						if view := views[current.ID]; view == currentView {
							view.reconnectAt = time.Time{}
						}
						mu.Unlock()
						if isReconnectActive(currentView, gen) && currentView.connectedOnce {
							updateDisconnectOverlay()
						}
					}
					if ctx.Err() != nil {
						return
					}
				} else if isReconnectActive(currentView, gen) && currentView.connectedOnce {
					updateDisconnectOverlay()
				}
				if m.OnReconnect != nil {
					m.OnReconnect(current.ID, attempt)
				}
				if m.Gate != nil {
					if err := m.Gate.Wait(ctx); err != nil {
						return
					}
				}
				mu.Lock()
				latest := views[current.ID]
				if latest != currentView {
					mu.Unlock()
					return
				}
				currentView.connecting = true
				mu.Unlock()
				nextView, err := connectView(current, visible, currentView)
				if err != nil {
					continue
				}
				mu.Lock()
				if len(currentView.pendingOps) > 0 {
					nextView.pendingOps = append(nextView.pendingOps, currentView.pendingOps...)
					currentView.pendingOps = nil
				}
				views[current.ID] = nextView
				mu.Unlock()
				if nextView.client != nil {
					nextView.client.RenderCurrent()
				}
				return
			}
		}(session, view)
		return view, nil
	}

	activateView = func(nextID string) error {
		if nextID == "" {
			return nil
		}
		mu.Lock()
		delete(removedSessions, nextID)
		var nextSession *SessionInfo
		for i := range sessions {
			if sessions[i].ID == nextID {
				nextSession = &sessions[i]
				break
			}
		}
		if nextSession == nil {
			mu.Unlock()
			return fmt.Errorf("session %q not found", nextID)
		}
		changedActive := activeID != nextID
		if view, ok := views[nextID]; ok && view.client != nil {
			reconnect := !view.connected && !view.connecting
			if activeID != nextID {
				if activeView, ok := views[activeID]; ok {
					activeView.visible = false
					activeView.hiddenAt = m.Clock.Now()
					if activeView.client != nil {
						activeView.client.SetStdout(io.Discard)
					}
				}
				activeID = nextID
			}
			view.visible = true
			view.hiddenAt = time.Time{}
			view.client.SetStdout(stdout)
			if reconnect && view.cancel != nil {
				reconnectAt := view.reconnectAt
				reconnectGen := view.reconnectGen
				view.cancel()
				nextView, err := connectView(*nextSession, true, view)
				if err != nil {
					return err
				}
				if len(view.pendingOps) > 0 {
					nextView.pendingOps = append(nextView.pendingOps, view.pendingOps...)
					view.pendingOps = nil
				}
				if !reconnectAt.IsZero() {
					nextView.reconnectAt = reconnectAt
					nextView.reconnectGen = reconnectGen
				}
				views[nextID] = nextView
				activeID = nextID
				mu.Unlock()
				setTabs()
				if changedActive {
					ui.ApplyAction(mvu.TabWakeAction{Duration: 0})
					if nextView.client != nil {
						nextView.client.ForceTabsVisibleOnce()
					}
				}
				renderActiveCurrent()
				updateDisconnectOverlay()
				if localSessionMode {
					syncActiveRoutedStatus()
				}
				if m.OnActive != nil && changedActive {
					m.OnActive(nextID)
				}
				return nil
			}
			mu.Unlock()
			setTabs()
			if changedActive {
				ui.ApplyAction(mvu.TabWakeAction{Duration: 0})
				if view.client != nil {
					view.client.ForceTabsVisibleOnce()
				}
			}
			if changedActive && view.client != nil && view.client.HasSnapshot() {
				// Reset per-view delta baseline when a hidden view becomes visible so
				// the terminal receives a complete frame swap immediately.
				view.client.RenderCurrentFull()
			} else {
				renderActiveCurrent()
			}
			updateDisconnectOverlay()
			if localSessionMode {
				syncActiveRoutedStatus()
			}
			if m.OnActive != nil && changedActive {
				m.OnActive(nextID)
			}
			return nil
		}

		var prevView *sessionView
		if activeID != nextID {
			if activeView, ok := views[activeID]; ok {
				prevView = activeView
				activeView.visible = false
				activeView.hiddenAt = m.Clock.Now()
				if activeView.client != nil {
					activeView.client.SetStdout(io.Discard)
				}
			}
		}

		view, err := connectView(*nextSession, true, prevView)
		if err != nil {
			mu.Unlock()
			return err
		}
		views[nextID] = view
		activeID = nextID
		mu.Unlock()
		setTabs()
		if changedActive {
			ui.ApplyAction(mvu.TabWakeAction{Duration: 0})
			if view.client != nil {
				view.client.ForceTabsVisibleOnce()
			}
		}
		renderActiveCurrent()
		updateDisconnectOverlay()
		if localSessionMode {
			syncActiveRoutedStatus()
		}
		if m.OnActive != nil && changedActive {
			m.OnActive(nextID)
		}
		return nil
	}

	if err := activateView(activeID); err != nil {
		return err
	}
	renderTabs()

	applySessions = func(updated []SessionInfo) {
		if m.Logger != nil {
			m.Logger.Debug("attach.sessions.apply", "count", len(updated))
		}
		waitMu.Lock()
		allowWait := waitCtrl.CanStart()
		waitActive := waitCtrl.Waiting()
		waitMu.Unlock()
		transientEmpty := false
		mu.Lock()
		for _, view := range views {
			if view == nil || view.removed {
				continue
			}
			if view.connecting {
				transientEmpty = true
				break
			}
			if view.client == nil {
				continue
			}
			if err := view.client.ReadErr(); err != nil && !isTerminalHostError(err) {
				transientEmpty = true
				break
			}
		}
		mu.Unlock()
		if !localSessionMode && len(updated) == 0 && (allowWait || waitActive || isOffline() || transientEmpty) {
			if !waitActive {
				startWaitForSessions()
			} else {
				showWaitOverlay()
			}
			return
		}
		if len(updated) > 0 {
			waitMu.Lock()
			waitCtrl.ClearAllowance()
			waitMu.Unlock()
		}
		allIDs := make(map[string]struct{}, len(updated))
		for _, s := range updated {
			allIDs[s.ID] = struct{}{}
		}
		var nextActive string
		var needsConnect bool
		var needsActivate bool
		var shouldExit bool
		mu.Lock()
		removed := 0
		for id, view := range views {
			if _, ok := allIDs[id]; ok {
				continue
			}
			if localSessionMode {
				routedStatusMu.Lock()
				delete(routedStatuses, id)
				routedStatusMu.Unlock()
			}
			view.removed = true
			removedSessions[id] = struct{}{}
			if view.cancel != nil {
				view.cancel()
			}
			removed++
			delete(views, id)
		}
		sessions = updated
		if activeID != "" && !mvu.SessionIDExists(mvu.SessionTabSourcesFrom(sessions), activeID) {
			activeID = m.pickActiveSession(sessions)
		}
		if m.Logger != nil {
			m.Logger.Debug("attach.sessions.apply.done", "removed", removed, "active", activeID)
		}
		nextActive = activeID
		if nextActive != "" {
			if view := views[nextActive]; view == nil || view.client == nil || (!view.connected && !view.connecting) {
				needsConnect = true
			} else if !view.visible {
				needsActivate = true
			}
		}
		if len(sessions) == 0 {
			shouldExit = true
		}
		mu.Unlock()
		if len(sessions) > 0 {
			stopWaitForSessions()
			if !localSessionMode && !isOffline() {
				_ = ui.ApplyAction(mvu.AttachConnectivityAction{Input: mvu.AttachConnectivityInput{
					Connected: true,
					Endpoint:  endpointLabel,
					Now:       m.Clock.Now(),
				}})
			}
		}
		setTabs()
		if localSessionMode {
			syncActiveRoutedStatus()
		}
		if shouldExit {
			m.setFatal(errNoSessions)
			cancel()
			return
		}
		if needsConnect || needsActivate {
			_ = activateView(nextActive)
		}
		renderTabs()
	}

	refreshSessions = func() int {
		refreshMu.Lock()
		defer refreshMu.Unlock()
		now := m.Clock.Now()
		if !lastRefresh.IsZero() && now.Sub(lastRefresh) < time.Second {
			if m.Logger != nil {
				m.Logger.Trace("attach.sessions.refresh.skip", "since", now.Sub(lastRefresh))
			}
			return -1
		}
		if m.Gate != nil && !m.Gate.Allowed() {
			if m.Logger != nil {
				m.Logger.Trace("attach.sessions.refresh.gated")
			}
			return -1
		}
		lastRefresh = now
		updated, err := m.fetchSessions(ctx, httpURL)
		if err != nil {
			if errors.Is(err, ErrAuthExpired) {
				m.setFatal(err)
			}
			if m.Logger != nil {
				m.Logger.Debug("attach.sessions.refresh.failed", "err", err)
			}
			return -1
		}
		if m.Logger != nil {
			m.Logger.Debug("attach.sessions.refresh.ok", "count", len(updated))
		}
		applySessions(updated)
		return len(updated)
	}
	forceRefreshSessions = func() int {
		refreshMu.Lock()
		lastRefresh = time.Time{}
		refreshMu.Unlock()
		return refreshSessions()
	}

	var refreshTicker *clock.Ticker
	if m.RefreshInterval > 0 {
		refreshTicker = m.Clock.NewTicker(m.RefreshInterval)
		defer refreshTicker.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-refreshTicker.C:
				}
				refreshSessions()
			}
		}()
	}
	if m.SessionEvents != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-m.SessionEvents:
					if !ok {
						return
					}
					forceRefreshSessions()
				}
			}
		}()
	}

	if !localSessionMode {
		overlayTicker := m.Clock.NewTicker(time.Second)
		defer overlayTicker.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-overlayTicker.C:
				}
				updateDisconnectOverlay()
			}
		}()
	}

	idleTicker := m.Clock.NewTicker(time.Second)
	defer idleTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-idleTicker.C:
			}
			now := m.Clock.Now()
			mu.Lock()
			for id, view := range views {
				if view.visible || view.hiddenAt.IsZero() {
					continue
				}
				if now.Sub(view.hiddenAt) < m.InactiveTTL {
					continue
				}
				view.removed = true
				removedSessions[id] = struct{}{}
				if view.cancel != nil {
					view.cancel()
				}
				if m.Logger != nil {
					m.Logger.Debug("attach.view.inactive.remove", "session", id)
				}
				delete(views, id)
			}
			mu.Unlock()
		}
	}()

	if stdinFile, ok := stdin.(*os.File); ok && term.IsTerminal(int(stdinFile.Fd())) {
		stdinState, err := term.MakeRaw(int(stdinFile.Fd()))
		if err != nil {
			return err
		}
		defer func() {
			_ = term.Restore(int(stdinFile.Fd()), stdinState)
		}()
	}

	go func() {
		m.handleResize(ctx, &mu, &views, func() string { return activeID })
	}()

	reader := bufio.NewReader(stdin)
	readCh := make(chan []byte, 1)
	readErrCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := reader.Read(buf)
			if err != nil {
				readErrCh <- err
				return
			}
			if n == 0 {
				continue
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			readCh <- data
		}
	}()
	var prefix control.Prefix
	var scrollState scrollInputState
	var mouseFilter mouseReportFilter
	resolveNextTab := func(delta int) (string, string) {
		mu.Lock()
		current := make([]SessionInfo, len(sessions))
		copy(current, sessions)
		active := activeID
		mu.Unlock()
		next := mvu.NextSessionID(mvu.SessionTabSourcesFrom(current), active, delta)
		if next != active && next != "" {
			return next, active
		}
		refreshed := -1
		if refreshSessions != nil {
			refreshed = refreshSessions()
		}
		// A throttled refresh can leave next/active stale during rapid tab-switch input.
		// Force one refresh attempt before treating the switch as a true no-op.
		if refreshed < 0 && forceRefreshSessions != nil {
			refreshed = forceRefreshSessions()
		}
		if refreshed >= 0 {
			mu.Lock()
			current = make([]SessionInfo, len(sessions))
			copy(current, sessions)
			active = activeID
			mu.Unlock()
			next = mvu.NextSessionID(mvu.SessionTabSourcesFrom(current), active, delta)
		}
		return next, active
	}
	wakeTabsForNoOpSwitch := func() {
		_, client, _, _, _ := activeViewSnapshot()
		if client == nil || !client.HasSnapshot() {
			return
		}
		ui.ApplyAction(mvu.TabWakeAction{Duration: 0})
		client.ForceTabsVisibleOnce()
		client.RenderCurrent()
	}
	for {
		select {
		case <-ctx.Done():
			if err := m.fatal(); err != nil {
				return err
			}
			return ctx.Err()
		case err := <-readErrCh:
			if err != io.EOF {
				m.Logger.Debug("attach.stdin.read.failed", "err", err)
			}
			if fatal := m.fatal(); fatal != nil {
				return fatal
			}
			return err
		case data := <-readCh:
			if len(data) == 0 {
				continue
			}
			if m.Logger != nil && len(data) > 0 {
				m.Logger.Debug("attach.stdin.read", "bytes", len(data))
			}
			processNormalByte := func(b byte) bool {
				_, client, _, _, _ := activeViewSnapshot()
				helpVisible := ui.Read().HelpVisible
				if uiAction, ok := mvu.ActionForHelpDismissKey(helpVisible, b); ok {
					ui.ApplyAction(uiAction)
					renderActiveCurrent()
					return true
				}
				// Help modal is input-modal: consume all non-dismiss keys.
				if helpVisible {
					return true
				}
				if b == 0x04 {
					cancel()
					return false
				}
				action, out := prefix.Feed(b)
				cmdKind := protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED
				cmdSet := false
				if action != control.ActionNone {
					switch action {
					case control.ActionHelp:
						ui.ApplyAction(mvu.HelpVisibleAction{Visible: true})
						renderActiveCurrent()
						return true
					case control.ActionToggleTabBar:
						ui.ApplyAction(mvu.TabToggleAction{})
						renderActiveCurrent()
						return true
					}
					if uiAction, ok := mvu.ActionForControl(action); ok {
						ui.ApplyAction(uiAction)
						renderActiveCurrent()
						return true
					}
					switch action {
					case control.ActionQuit:
						cancel()
						return false
					case control.ActionSendCtrlD:
						cmdKind = protocolpb.CommandKind_COMMAND_KIND_SEND_EOF
						cmdSet = true
					case control.ActionScrollback:
						if client != nil {
							client.SetScrollbackActive(true)
							client.RenderCurrent()
						}
					case control.ActionNewPTY:
						// Attach does not support spawning host local PTY sessions.
						// Keep Ctrl+L c as a no-op in both relay and local-headless modes.
						return true
					case control.ActionNextTab:
						next, active := resolveNextTab(1)
						if next == "" || next == active {
							wakeTabsForNoOpSwitch()
							return true
						}
						_ = activateView(next)
					case control.ActionPrevTab:
						next, active := resolveNextTab(-1)
						if next == "" || next == active {
							wakeTabsForNoOpSwitch()
							return true
						}
						_ = activateView(next)
					case control.ActionToggleRespawn:
						if localSessionMode {
							cmdKind = protocolpb.CommandKind_COMMAND_KIND_TOGGLE_RESPAWN
							cmdSet = true
						}
					case control.ActionToggleWallInactivity:
						if localSessionMode {
							cmdKind = protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY
							cmdSet = true
							break
						}
						targetView, _, _, _, _ := activeViewSnapshot()
						if targetView == nil || strings.TrimSpace(targetView.id) == "" {
							showConnected("wall inactivity toggle unavailable", 2*time.Second)
							return true
						}
						targetID := targetView.id
						tokenValue := strings.TrimSpace(m.AccessToken)
						if tokenValue == "" && m.TokenRefresher != nil {
							refreshed, err := m.refreshToken(ctx)
							if err != nil {
								showError("wall inactivity toggle failed: token refresh", 2*time.Second)
								return true
							}
							tokenValue = strings.TrimSpace(refreshed)
						}
						if tokenValue == "" {
							showConnected("wall inactivity requires authentication", 2*time.Second)
							return true
						}
						resp, err := relayclient.ToggleWallInactivity(
							ctx,
							m.Endpoint,
							tokenValue,
							targetID,
							m.TLSDir,
							m.Insecure,
						)
						if err != nil {
							showError("wall inactivity toggle failed", 2*time.Second)
							return true
						}
						if resp.Enabled {
							status := "wall inactivity on"
							if label := strings.TrimSpace(resp.InactiveAfter); label != "" {
								status = "wall inactivity " + label
							}
							showConnected(status, 2*time.Second)
						} else {
							showConnected("wall inactivity off", 2*time.Second)
						}
					case control.ActionToggleOffline:
						if localSessionMode && m.AllowOfflineToggle {
							cmdKind = protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE
							cmdSet = true
							clearActiveRoutedStatus()
							if toggleActiveSessionOffline() {
								setTabs()
								syncActiveRoutedStatus()
								wakeTabsForNoOpSwitch()
							}
						} else {
							showError("offline toggle is host local-only", 2*time.Second)
							return true
						}
					case control.ActionNextTheme:
						setTheme(nextThemeName(themeName), true)
					}
					if !cmdSet && len(out) == 0 {
						return true
					}
				}
				if cmdSet {
					mu.Lock()
					view := views[activeID]
					if view == nil && len(views) == 1 {
						for _, v := range views {
							view = v
							break
						}
					}
					targetClient := (*Client)(nil)
					if view != nil {
						targetClient = view.client
					}
					connected := view != nil && view.connected
					flushing := view != nil && view.flushingInput
					viewID := activeID
					if view != nil {
						viewID = view.id
					}
					mu.Unlock()
					if view == nil || targetClient == nil {
						if m.Logger != nil {
							m.Logger.Debug("attach.stdin.no.view.command", "session", activeID)
						}
						return true
					}
					if !connected || flushing {
						var immediateClient *Client
						mu.Lock()
						current := views[viewID]
						if current == nil && len(views) == 1 {
							for _, v := range views {
								current = v
								break
							}
						}
						switch {
						case current == nil || current.client == nil:
						case current.connected && !current.flushingInput:
							immediateClient = current.client
						default:
							current.pendingOps = append(current.pendingOps, pendingOp{command: cmdKind})
						}
						mu.Unlock()
						if immediateClient == nil {
							return true
						}
						targetClient = immediateClient
					}
					if err := targetClient.SendCommand(ctx, cmdKind); err != nil {
						if m.Logger != nil {
							m.Logger.Debug("attach.stdin.command.send.failed", "err", err, "kind", cmdKind.String(), "session", activeID)
						}
						mu.Lock()
						current := views[viewID]
						if current != nil {
							current.pendingOps = append(current.pendingOps, pendingOp{command: cmdKind})
						}
						mu.Unlock()
						return true
					}
					return true
				}
				if len(out) == 0 {
					return true
				}
				mu.Lock()
				view := views[activeID]
				if view == nil && len(views) == 1 {
					for _, v := range views {
						view = v
						break
					}
				}
				connected := view != nil && view.connected
				connecting := view != nil && view.connecting
				flushing := view != nil && view.flushingInput
				viewID := activeID
				if view != nil {
					viewID = view.id
				}
				targetClient := (*Client)(nil)
				if view != nil {
					targetClient = view.client
				}
				mu.Unlock()
				if view == nil || targetClient == nil {
					if m.Logger != nil {
						m.Logger.Debug("attach.stdin.no.view", "session", activeID, "hasView", view != nil, "hasClient", targetClient != nil)
					}
					return true
				}
				if !connected || flushing {
					if m.Logger != nil {
						m.Logger.Debug("attach.stdin.dropped.disconnected", "session", activeID, "connecting", connecting, "flushing", flushing)
					}
					var immediateClient *Client
					queued := append([]byte(nil), out...)
					mu.Lock()
					current := views[viewID]
					if current == nil && len(views) == 1 {
						for _, v := range views {
							current = v
							break
						}
					}
					switch {
					case current == nil || current.client == nil:
					case current.connected && !current.flushingInput:
						immediateClient = current.client
					default:
						current.pendingOps = append(current.pendingOps, pendingOp{input: queued})
					}
					mu.Unlock()
					if immediateClient == nil {
						return true
					}
					targetClient = immediateClient
				}
				if m.Logger != nil {
					m.Logger.Debug("attach.stdin.send", "bytes", len(out))
				}
				if len(out) == 1 && out[0] == 0x0c {
					targetClient.PrepareForCtrlLClear()
				}
				if err := targetClient.SendInput(ctx, out); err != nil {
					if m.Logger != nil {
						m.Logger.Debug("attach.stdin.send.failed", "err", err)
					}
				}
				return true
			}
			filtered := make([]byte, 0, 8)
			for _, b := range data {
				_, client, _, _, _ := activeViewSnapshot()
				if client != nil && client.ScrollbackActive() {
					cmd := scrollState.feed(b)
					if cmd == scrollExit {
						client.SetScrollbackActive(false)
						client.RenderCurrent()
						continue
					}
					if cmd != scrollNone {
						_, rows := client.terminalSize()
						if rows <= 0 {
							rows = config.DefaultTerminalRows
						}
						half := rows / 2
						if half < 1 {
							half = 1
						}
						changed := false
						switch cmd {
						case scrollPageUp:
							changed = client.ScrollbackPage(1, half)
						case scrollPageDown:
							changed = client.ScrollbackPage(-1, half)
						case scrollLineUp:
							changed = client.ScrollbackPage(1, 1)
						case scrollLineDown:
							changed = client.ScrollbackPage(-1, 1)
						case scrollTop:
							client.ScrollbackTop(rows)
							changed = true
						case scrollBottom:
							client.ScrollbackBottom()
							changed = true
						case scrollWheelUp:
							changed = client.ScrollbackPage(1, 3)
						case scrollWheelDown:
							changed = client.ScrollbackPage(-1, 3)
						}
						if changed {
							client.RenderCurrent()
						}
					}
					continue
				}
				filtered = filterMouseByte(&mouseFilter, b, filtered)
				for _, fb := range filtered {
					if !processNormalByte(fb) {
						return nil
					}
				}
			}
		}
	}
}

func (m *MultiClient) handleResize(ctx context.Context, mu *sync.Mutex, views *map[string]*sessionView, activeID func() string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
		}
		id := activeID()
		mu.Lock()
		view := (*views)[id]
		mu.Unlock()
		if view == nil || view.client == nil {
			continue
		}
		cols, rows := view.client.terminalSize()
		if cols == 0 || rows == 0 {
			cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
		}
		view.client.RenderCurrent()
		if m.SessionSource != nil || view.client.isController() {
			_ = view.client.SendResize(ctx, cols, rows)
		}
	}
}

func (m *MultiClient) fetchSessions(ctx context.Context, httpURL string) ([]SessionInfo, error) {
	if m.SessionSource != nil {
		sessions, err := m.SessionSource(ctx)
		if err != nil {
			return nil, err
		}
		if sessions == nil {
			sessions = []SessionInfo{}
		}
		mvu.SortSessionsByLastActive(sessions)
		return sessions, nil
	}
	if m.Gate != nil {
		if err := m.Gate.Wait(ctx); err != nil {
			return nil, err
		}
	}
	if m.TokenRefresher != nil {
		if _, err := m.refreshToken(ctx); err != nil {
			return nil, authExpiredError(m.Endpoint, err)
		}
	}
	tlsCfg, err := clientTLSConfig(m.TLSDir, m.Insecure)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL+"/sessions", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+m.AccessToken)
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			if attempt == 0 {
				if _, refreshErr := m.refreshToken(ctx); refreshErr == nil {
					_ = resp.Body.Close()
					continue
				} else {
					_ = resp.Body.Close()
					return nil, authExpiredError(m.Endpoint, refreshErr)
				}
			}
			msg := authErrorMessage(resp)
			_ = resp.Body.Close()
			return nil, authExpiredError(m.Endpoint, fmt.Errorf("%s", msg))
		}
		break
	}
	if resp == nil {
		return nil, fmt.Errorf("list sessions failed: no response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions failed: %s", resp.Status)
	}
	var sessions []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	mvu.SortSessionsByLastActive(sessions)
	return sessions, nil
}

func (m *MultiClient) pickActiveSession(sessions []SessionInfo) string {
	if m.SessionID != "" {
		for _, s := range sessions {
			if s.ID == m.SessionID {
				return s.ID
			}
		}
	}
	for _, s := range sessions {
		if s.Status == "active" {
			return s.ID
		}
	}
	if len(sessions) > 0 {
		return sessions[0].ID
	}
	return ""
}

func (m *MultiClient) stdinReader() io.Reader {
	if m.Stdin != nil {
		return m.Stdin
	}
	return os.Stdin
}

func (m *MultiClient) refreshToken(ctx context.Context) (string, error) {
	if m.TokenRefresher == nil {
		return "", fmt.Errorf("token refresh unavailable")
	}
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	token, err := m.TokenRefresher(ctx)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("refresh returned empty token")
	}
	m.AccessToken = token
	return token, nil
}

func (m *MultiClient) setFatal(err error) {
	if err == nil {
		return
	}
	m.fatalMu.Lock()
	defer m.fatalMu.Unlock()
	if m.fatalErr != nil {
		return
	}
	m.fatalErr = err
	if m.cancel != nil {
		m.cancel()
	}
	if m.stdinCloser != nil {
		_ = m.stdinCloser.Close()
	}
}

func (m *MultiClient) fatal() error {
	m.fatalMu.Lock()
	defer m.fatalMu.Unlock()
	return m.fatalErr
}

func (m *MultiClient) stdoutWriter() io.Writer {
	if m.Stdout != nil {
		return m.Stdout
	}
	return os.Stdout
}
