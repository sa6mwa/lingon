package session

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/retryafter"
	"pkt.systems/lingon/internal/theme"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

type remoteSessionInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	Headless     bool      `json:"headless,omitempty"`
	Status       string    `json:"status"`
	LastActiveAt time.Time `json:"last_active_at"`
}

func (s remoteSessionInfo) SessionTabID() string {
	return s.ID
}

func (s remoteSessionInfo) SessionTabName() string {
	return s.Name
}

func (s remoteSessionInfo) SessionTabLastActiveAt() time.Time {
	return s.LastActiveAt
}

type remoteView struct {
	id              string
	name            string
	client          *attach.Client
	cancel          context.CancelFunc
	visible         bool
	hiddenAt        time.Time
	missingSince    time.Time
	disabled        bool
	awaiting        bool
	running         bool
	sessionClosed   bool
	restart         bool
	needsFullRender bool
	stdout          io.Writer
	ctx             context.Context
	pending         []byte
	runID           uint64
}

type remoteOptions struct {
	DisableDesktopNotifications bool
	DesktopNotifier             desktopnotify.Notifier
	Endpoint                    string
	Token                       string
	TokenRefresher              func(context.Context) (string, error)
	HostnameOnly                bool
	LocalID                     string
	LocalName                   string
	TLSDir                      string
	Insecure                    bool
	Theme                       string
	Logger                      pslog.Logger
	Compositor                  *mvu.Runtime
	TermSize                    func() (int, int)
	Clock                       clock.Clock
	InactiveTTL                 time.Duration
	RefreshInterval             time.Duration
	Gate                        *netgate.Gate
	OnSessions                  func([]remoteSessionInfo)
	OnViewClosed                func(string, error)
	OnOverlayChange             func()
}

type remoteManager struct {
	endpoint                    string
	endpointLabel               string
	token                       string
	tokenRefresher              func(context.Context) (string, error)
	localID                     string
	localName                   string
	tlsDir                      string
	insecure                    bool
	logger                      pslog.Logger
	compositor                  *mvu.Runtime
	themeName                   string
	disableDesktopNotifications bool
	desktopNotifier             desktopnotify.Notifier
	termSize                    func() (int, int)
	clock                       clock.Clock
	inactiveTTL                 time.Duration
	refreshInterval             time.Duration
	onSessions                  func([]remoteSessionInfo)
	onViewClosed                func(string, error)
	onOverlayChange             func()
	gate                        *netgate.Gate

	mu           sync.Mutex
	refreshMu    sync.Mutex
	sessions     []remoteSessionInfo
	lastNonEmpty time.Time
	views        map[string]*remoteView
	retained     map[string]time.Time
	disabled     map[string]bool
	httpClient   *http.Client
	httpTr       *http.Transport
}

var remoteSessionsRequestTimeout = 12 * time.Second

const missingSessionGrace = 5 * time.Second

func newRemoteManager(opts remoteOptions) *remoteManager {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Default()
	}
	inactive := opts.InactiveTTL
	if inactive == 0 {
		inactive = 30 * time.Second
	}
	refresh := opts.RefreshInterval
	if refresh == 0 {
		refresh = 60 * time.Second
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.New()
	}
	themeName := resolveThemeName(opts.Theme)
	compositor := opts.Compositor
	if compositor == nil {
		compositor = mvu.NewRuntime()
	}
	compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:    clk,
		Endpoint: opts.Endpoint,
		Theme:    theme.TUI(themeName),
	}})
	return &remoteManager{
		endpoint:                    opts.Endpoint,
		endpointLabel:               config.EndpointDisplay(opts.Endpoint, opts.HostnameOnly),
		token:                       opts.Token,
		tokenRefresher:              opts.TokenRefresher,
		localID:                     opts.LocalID,
		localName:                   opts.LocalName,
		tlsDir:                      opts.TLSDir,
		insecure:                    opts.Insecure,
		logger:                      logger,
		compositor:                  compositor,
		themeName:                   themeName,
		disableDesktopNotifications: opts.DisableDesktopNotifications,
		desktopNotifier:             opts.DesktopNotifier,
		termSize:                    opts.TermSize,
		clock:                       clk,
		inactiveTTL:                 inactive,
		refreshInterval:             refresh,
		gate:                        opts.Gate,
		onSessions:                  opts.OnSessions,
		onViewClosed:                opts.OnViewClosed,
		onOverlayChange:             opts.OnOverlayChange,
		views:                       make(map[string]*remoteView),
		retained:                    make(map[string]time.Time),
		disabled:                    make(map[string]bool),
	}
}

func (m *remoteManager) SetTheme(name string) {
	resolved := resolveThemeName(name)
	m.mu.Lock()
	m.themeName = resolved
	m.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Theme: theme.TUI(resolved)}})
	for _, view := range m.views {
		if view.client != nil {
			view.client.SetTheme(resolved)
		}
	}
	m.mu.Unlock()
}

func (m *remoteManager) refreshToken(ctx context.Context) bool {
	_, err := m.refreshTokenErr(ctx)
	return err == nil
}

func (m *remoteManager) refreshTokenErr(ctx context.Context) (string, error) {
	if m == nil || m.tokenRefresher == nil {
		return "", fmt.Errorf("token refresh unavailable")
	}
	token, err := m.tokenRefresher(ctx)
	if err != nil {
		m.logger.Warn("session.remote.token.refresh.failed", "err", err)
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("refresh returned empty token")
	}
	m.setToken(token)
	return token, nil
}

func (m *remoteManager) setToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = token
	for _, view := range m.views {
		if view != nil && view.client != nil {
			view.client.AccessToken = token
		}
	}
}

func (m *remoteManager) logViewState(event string, view *remoteView, err error) {
	if m == nil || m.logger == nil {
		return
	}
	if view == nil {
		m.logger.Trace("session.remote.view.state", "event", event, "view", "nil")
		return
	}
	m.logger.Trace(
		"session.remote.view.state",
		"event", event,
		"session", view.id,
		"visible", view.visible,
		"awaiting", view.awaiting,
		"disabled", view.disabled,
		"running", view.running,
		"restart", view.restart,
		"run_id", view.runID,
		"hidden_at", view.hiddenAt,
		"has_client", view.client != nil,
		"err", err,
	)
}

func (m *remoteManager) Start(ctx context.Context) {
	go func() {
		<-ctx.Done()
		m.closeHTTPClient()
	}()

	refreshTicker := m.clock.NewTicker(m.refreshInterval)
	idleTicker := m.clock.NewTicker(time.Second)

	go func() {
		defer refreshTicker.Stop()
		if err := m.refreshSessions(ctx); err != nil {
			m.logger.Debug("session.remote.refresh.failed", "err", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-refreshTicker.C:
			}
			if err := m.refreshSessions(ctx); err != nil {
				m.logger.Debug("session.remote.refresh.failed", "err", err)
			}
		}
	}()

	go func() {
		defer idleTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-idleTicker.C:
			}
			now := m.clock.Now()
			m.mu.Lock()
			for _, view := range m.views {
				if view.visible || view.hiddenAt.IsZero() {
					continue
				}
				if now.Sub(view.hiddenAt) < m.inactiveTTL {
					continue
				}
				if view.cancel != nil {
					view.cancel()
				}
				delete(m.views, view.id)
			}
			m.mu.Unlock()
		}
	}()
}

func (m *remoteManager) Sessions() []remoteSessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copySessions(m.sessions)
}

func (m *remoteManager) DisabledSessions() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]bool, len(m.disabled))
	for id, disabled := range m.disabled {
		if disabled {
			out[id] = true
		}
	}
	for id, view := range m.views {
		if view != nil && view.disabled {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *remoteManager) HasSession(sessionID string) bool {
	if m == nil || sessionID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if session.ID == sessionID {
			return true
		}
	}
	if view := m.views[sessionID]; view != nil {
		return true
	}
	return m.disabled[sessionID]
}

func (m *remoteManager) IsDisabled(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if view := m.views[sessionID]; view != nil {
		if view.disabled || view.awaiting {
			return true
		}
	}
	return m.disabled[sessionID]
}

func (m *remoteManager) Disable(sessionID string) {
	if sessionID == "" {
		return
	}
	var view *remoteView
	var callback func([]remoteSessionInfo)
	var sessions []remoteSessionInfo
	m.mu.Lock()
	m.disabled[sessionID] = true
	view = m.views[sessionID]
	sessions = copySessions(m.sessions)
	callback = m.onSessions
	if view != nil {
		view.disabled = true
		view.awaiting = false
		view.restart = false
		view.visible = false
		view.hiddenAt = m.clock.Now()
		view.pending = view.pending[:0]
		view.stdout = nil
		view.ctx = nil
		if view.cancel != nil {
			view.cancel()
			view.cancel = nil
		}
	}
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.Close("disabled")
		view.client.SetStdout(io.Discard)
	}
	m.logViewState("disable", view, nil)
	if callback != nil && len(sessions) > 0 {
		callback(sessions)
	}
}

func (m *remoteManager) Enable(ctx context.Context, sessionID string, stdout io.Writer) {
	if sessionID == "" {
		return
	}
	var callback func([]remoteSessionInfo)
	var sessions []remoteSessionInfo
	var view *remoteView
	var oldClient *attach.Client
	var oldCancel context.CancelFunc
	m.mu.Lock()
	view = m.views[sessionID]
	sessions = copySessions(m.sessions)
	callback = m.onSessions
	if view != nil {
		if view.client != nil {
			oldClient = view.client
			oldCancel = view.cancel
			view.runID++
			view.client = nil
			view.cancel = nil
			view.running = false
		}
		view.disabled = true
		view.awaiting = true
		view.restart = true
		view.ctx = ctx
		view.stdout = stdout
	}
	m.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldClient != nil {
		oldClient.Close("reconnect")
	}
	if view == nil {
		view = &remoteView{id: sessionID, disabled: true, awaiting: true, restart: true, ctx: ctx, stdout: stdout}
		m.mu.Lock()
		m.views[sessionID] = view
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.disabled[sessionID] = true
	view.visible = true
	view.hiddenAt = time.Time{}
	m.mu.Unlock()
	m.logViewState("enable", view, nil)

	if err := m.connectView(ctx, view, io.Discard, func() {
		m.markReady(view.id)
	}); err != nil {
		m.logger.Debug("session.remote.reconnect.failed", "session", view.id, "err", err)
	}
	if view != nil && view.client != nil {
		go func(id string, client *attach.Client, waitCtx context.Context) {
			timer := m.clock.NewTimer(750 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-waitCtx.Done():
				return
			case <-timer.C:
			}
			if client.Connected() {
				m.markReady(id)
			}
		}(view.id, view.client, ctx)
	}

	if callback != nil && len(sessions) > 0 {
		callback(sessions)
	}
}

func (m *remoteManager) markReady(sessionID string) {
	var view *remoteView
	var stdout io.Writer
	var callback func([]remoteSessionInfo)
	var sessions []remoteSessionInfo
	var showConnected func()
	connectedDelay := 2 * time.Second
	m.mu.Lock()
	view = m.views[sessionID]
	if view == nil || !view.awaiting {
		m.mu.Unlock()
		return
	}
	view.awaiting = false
	view.disabled = false
	view.restart = false
	delete(m.disabled, sessionID)
	stdout = view.stdout
	sessions = copySessions(m.sessions)
	callback = m.onSessions
	showConnected = func() {
		effect := m.compositor.ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
			Kind:     mvu.StatusConnected,
			Endpoint: m.endpointLabel,
			Duration: 2 * time.Second,
		}})
		connectedDelay = effect.Delay
	}
	m.mu.Unlock()
	m.logViewState("mark_ready", view, nil)

	if view.client != nil {
		showConnected()
		if stdout != nil {
			view.client.SetStdout(stdout)
		}
		view.client.RenderCurrent()
		if connectedDelay > 0 {
			m.scheduleConnectedClear(view, connectedDelay)
		}
		m.flushPending(view)
	}
	if callback != nil && len(sessions) > 0 {
		callback(sessions)
	}
}

func (m *remoteManager) scheduleConnectedClear(view *remoteView, d time.Duration) {
	if view == nil || d <= 0 {
		return
	}
	ctx := view.ctx
	runID := view.runID
	id := view.id
	go func() {
		timer := m.clock.NewTimer(d)
		defer timer.Stop()
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		} else {
			<-timer.C
		}
		m.mu.Lock()
		current := m.views[id]
		if current != view || view.runID != runID {
			m.mu.Unlock()
			return
		}
		client := view.client
		disabled := view.disabled || view.awaiting
		m.mu.Unlock()
		if client == nil {
			return
		}
		if disabled {
			client.RenderDisabled()
			return
		}
		client.RenderCurrent()
	}()
}

func (m *remoteManager) NextSessionID(active string, dir int) string {
	m.mu.Lock()
	sessions := copySessions(m.sessions)
	m.mu.Unlock()
	return mvu.NextSessionID(mvu.SessionTabSourcesFrom(sessions), active, dir)
}

func (m *remoteManager) Show(ctx context.Context, sessionID string, stdout io.Writer) (*remoteView, error) {
	if sessionID == "" || sessionID == m.localID {
		return nil, nil
	}
	onReady := func() {
		m.mu.Lock()
		view := m.views[sessionID]
		visible := view != nil && view.visible
		client := (*attach.Client)(nil)
		headless := false
		if view != nil {
			client = view.client
			view.sessionClosed = false
		}
		for _, info := range m.sessions {
			if info.ID == sessionID {
				headless = info.Headless
				break
			}
		}
		m.mu.Unlock()
		if visible && client != nil {
			if headless {
				_ = m.sendHeadlessResize(ctx, sessionID)
			}
			client.RenderCurrent()
		}
	}
	if m.gate != nil && !m.gate.Allowed() {
		m.mu.Lock()
		view := m.views[sessionID]
		if view == nil {
			view = &remoteView{id: sessionID}
			m.views[sessionID] = view
		}
		view.awaiting = true
		view.visible = true
		view.hiddenAt = time.Time{}
		view.needsFullRender = true
		view.ctx = ctx
		view.stdout = stdout
		m.mu.Unlock()
		m.deferConnect(ctx, view, stdout, onReady)
		return view, nil
	}
	m.mu.Lock()
	view := m.views[sessionID]
	disabled := m.disabled[sessionID]
	if view != nil && view.disabled {
		disabled = true
	}
	var pending []byte
	if view != nil {
		pending = append(pending, view.pending...)
	}
	if disabled {
		if view == nil {
			view = &remoteView{id: sessionID, disabled: true}
			m.views[sessionID] = view
		}
		view.visible = true
		view.hiddenAt = time.Time{}
		m.mu.Unlock()
		if view.client != nil {
			view.client.SetStdout(stdout)
			view.client.RenderDisabled()
		}
		return view, nil
	}
	if view != nil && view.client != nil && !view.disabled {
		if view.awaiting {
			// fall through to reconnect path
		} else if view.client.Connected() {
			view.visible = true
			view.hiddenAt = time.Time{}
			view.needsFullRender = true
			m.mu.Unlock()
			m.logViewState("show_existing", view, nil)
			view.client.SetStdout(stdout)
			return view, nil
		} else {
			view.awaiting = true
			view.restart = true
			if view.cancel != nil {
				view.cancel()
			}
		}
	}
	session, ok := sessionByID(m.sessions, sessionID)
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	if view == nil {
		view = &remoteView{id: session.ID}
	}
	view.name = mvu.SessionLabel(session.ID, session.Name)
	view.visible = true
	view.pending = pending
	view.needsFullRender = true

	m.mu.Lock()
	m.views[session.ID] = view
	m.mu.Unlock()
	m.logViewState("show_connect", view, nil)

	if err := m.connectView(ctx, view, stdout, onReady); err != nil {
		return nil, err
	}

	m.flushPending(view)
	return view, nil
}

func (m *remoteManager) connectView(ctx context.Context, view *remoteView, stdout io.Writer, onReady func()) error {
	if view == nil {
		return nil
	}
	if m.gate != nil && !m.gate.Allowed() {
		m.mu.Lock()
		view.awaiting = true
		view.ctx = ctx
		view.stdout = stdout
		m.mu.Unlock()
		m.deferConnect(ctx, view, stdout, onReady)
		return nil
	}
	m.mu.Lock()
	view.ctx = ctx
	viewRunning := view.running
	var seedFrom *attach.Client
	if view.client != nil && !viewRunning {
		seedFrom = view.client
		if view.missingSince.IsZero() {
			view.missingSince = m.clock.Now()
		}
		m.retained[view.id] = view.missingSince
		view.client = nil
	}
	if viewRunning {
		view.restart = true
		if seedFrom != nil {
			if view.cancel != nil {
				view.cancel()
			}
		}
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	m.logViewState("connect_view", view, nil)

	session, ok := sessionByID(m.sessions, view.id)
	if !ok {
		_ = m.refreshSessions(ctx)
		session, ok = sessionByID(m.sessions, view.id)
	}
	if !ok {
		session = remoteSessionInfo{ID: view.id, Name: view.name}
	}
	if view.client == nil {
		var tokenRefresher func(context.Context) (string, error)
		if m.tokenRefresher != nil {
			tokenRefresher = m.refreshTokenErr
		}
		client := &attach.Client{
			Endpoint:                    m.endpoint,
			SessionID:                   session.ID,
			AccessToken:                 m.token,
			RequestControl:              true,
			DisableResizePropagation:    true,
			DisableDesktopNotifications: m.disableDesktopNotifications,
			DesktopNotifier:             m.desktopNotifier,
			TLSDir:                      m.tlsDir,
			TermSize:                    m.termSize,
			Theme:                       m.themeName,
			Logger:                      m.logger,
			TokenRefresher:              tokenRefresher,
			Clock:                       m.clock,
		}
		if seedFrom != nil {
			client.SeedFrom(seedFrom)
		}
		client.OnOverlayStateChange = func() {
			m.notifyOverlayChange(session.ID)
		}
		client.OnControllerAcquired = func() {
			m.mu.Lock()
			current := m.views[session.ID]
			visible := current == view && view.visible
			m.mu.Unlock()
			if !visible {
				return
			}
			_ = m.sendHeadlessResize(ctx, session.ID)
		}
		client.OnSessionClosed = func(_ string) {
			m.handleExplicitSessionClosed(session.ID)
		}
		client.SetCompositor(m.compositor)
		view.client = client
	}
	view.name = mvu.SessionLabel(session.ID, session.Name)
	view.client.OnReady = onReady
	view.client.OnSessions = func(updated []attach.SessionInfo) {
		if len(updated) > 0 {
			m.applySessions(toRemoteSessions(updated))
		} else {
			m.applySessions(nil)
		}
	}
	view.client.SetStdout(stdout)

	m.mu.Lock()
	if view.cancel == nil {
		view.runID++
		runID := view.runID
		cctx, cancel := context.WithCancel(ctx)
		view.cancel = cancel
		view.running = true
		go func(v *remoteView, id uint64) {
			err := v.client.RunDetached(cctx)
			m.handleViewClosed(v, id, err)
		}(view, runID)
	}
	m.mu.Unlock()
	if seedFrom != nil && view.client != nil && view.visible {
		m.clock.AfterFunc(250*time.Millisecond, func() {
			m.mu.Lock()
			current := m.views[view.id]
			client := view.client
			visible := view.visible
			m.mu.Unlock()
			if current != view || client == nil || !visible || !client.Connected() {
				return
			}
			client.RenderCurrentFull()
		})
	}
	return nil
}

func (m *remoteManager) Hide(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	if view != nil {
		view.visible = false
		view.hiddenAt = m.clock.Now()
		view.needsFullRender = true
	}
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.SetStdout(io.Discard)
	}
	m.logViewState("hide", view, nil)
}

func (m *remoteManager) Render(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		if view.needsFullRender {
			view.needsFullRender = false
			view.client.RenderCurrentFull()
		} else {
			view.client.RenderCurrent()
		}
	}
	m.logViewState("render", view, nil)
}

func (m *remoteManager) RenderFull(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.RenderCurrentFull()
	}
	m.logViewState("render_full", view, nil)
}

func (m *remoteManager) RenderClear(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.RenderCurrentClear()
		view.needsFullRender = false
	}
	m.logViewState("render_clear", view, nil)
}

func (m *remoteManager) RenderDisabled(sessionID string, stdout io.Writer) {
	m.mu.Lock()
	view := m.views[sessionID]
	if view != nil {
		view.visible = true
		view.hiddenAt = time.Time{}
	}
	m.mu.Unlock()
	if view == nil || view.client == nil {
		return
	}
	m.logViewState("render_disabled", view, nil)
	view.client.SetStdout(stdout)
	view.needsFullRender = true
	view.client.RenderDisabled()
}

func (m *remoteManager) SetScrollbackActive(sessionID string, active bool) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.SetScrollbackActive(active)
		view.client.RenderCurrent()
	}
	m.logViewState("scrollback_active", view, nil)
}

func (m *remoteManager) ScrollbackPage(sessionID string, delta int, viewRows int) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		if view.client.ScrollbackPage(delta, viewRows) {
			view.client.RenderCurrent()
		}
	}
	m.logViewState("scrollback_page", view, nil)
}

func (m *remoteManager) ScrollbackPanX(sessionID string, delta int) bool {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		return view.client.ScrollbackPanX(delta)
	}
	return false
}

func (m *remoteManager) ScrollbackTop(sessionID string, viewRows int) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.ScrollbackTop(viewRows)
		view.client.RenderCurrent()
	}
	m.logViewState("scrollback_top", view, nil)
}

func (m *remoteManager) ScrollbackBottom(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.ScrollbackBottom()
		view.client.RenderCurrent()
	}
	m.logViewState("scrollback_bottom", view, nil)
}

func (m *remoteManager) ResetScrollback(sessionID string) {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view != nil && view.client != nil {
		view.client.ScrollbackReset()
		view.client.RenderCurrent()
	}
	m.logViewState("scrollback_reset", view, nil)
}

func (m *remoteManager) ScrollbackOffset(sessionID string) int {
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view == nil || view.client == nil {
		return 0
	}
	return view.client.ScrollbackOffset()
}

func (m *remoteManager) SendInput(ctx context.Context, sessionID string, data []byte, stdout io.Writer) error {
	if len(data) == 0 {
		return nil
	}
	m.mu.Lock()
	view := m.views[sessionID]
	disabled := m.disabled[sessionID]
	if view != nil && view.disabled {
		disabled = true
	}
	awaiting := view != nil && view.awaiting
	m.mu.Unlock()
	if disabled || awaiting {
		m.queueInput(sessionID, data)
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	if view == nil || view.client == nil {
		m.queueInput(sessionID, data)
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	if err := view.client.SendInput(ctx, data); err != nil {
		m.queueInput(sessionID, data)
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	m.logViewState("send_input", view, nil)
	return nil
}

func (m *remoteManager) SendCommand(ctx context.Context, sessionID string, kind protocolpb.CommandKind, stdout io.Writer) error {
	if kind == protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED {
		return nil
	}
	m.mu.Lock()
	view := m.views[sessionID]
	disabled := m.disabled[sessionID]
	if view != nil && view.disabled {
		disabled = true
	}
	awaiting := view != nil && view.awaiting
	m.mu.Unlock()
	if disabled || awaiting {
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	if view == nil || view.client == nil {
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	if err := view.client.SendCommand(ctx, kind); err != nil {
		m.Enable(ctx, sessionID, stdout)
		return nil
	}
	m.logViewState("send_command", view, nil)
	return nil
}

func (m *remoteManager) notifyOverlayChange(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	view := m.views[sessionID]
	visible := view != nil && view.visible
	callback := m.onOverlayChange
	m.mu.Unlock()
	if visible || callback == nil {
		return
	}
	callback()
}

func (m *remoteManager) deferConnect(ctx context.Context, view *remoteView, stdout io.Writer, onReady func()) {
	if m == nil || view == nil || m.gate == nil {
		return
	}
	viewID := view.id
	runID := view.runID
	go func() {
		if err := m.gate.Wait(ctx); err != nil {
			return
		}
		m.mu.Lock()
		current := m.views[viewID]
		if current != view || view.runID != runID {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		_ = m.connectView(ctx, view, stdout, onReady)
	}()
}

func (m *remoteManager) SendResize(ctx context.Context, sessionID string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	if !m.sessionAllowsResize(sessionID) {
		return nil
	}
	m.mu.Lock()
	view := m.views[sessionID]
	m.mu.Unlock()
	if view == nil || view.client == nil {
		return fmt.Errorf("session %q not connected", sessionID)
	}
	return view.client.SendResize(ctx, cols, rows)
}

func (m *remoteManager) sessionAllowsResize(sessionID string) bool {
	if m == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if session.ID == sessionID {
			return session.Headless
		}
	}
	return false
}

func (m *remoteManager) sendHeadlessResize(ctx context.Context, sessionID string) error {
	if m == nil || !m.sessionAllowsResize(sessionID) || m.termSize == nil {
		return nil
	}
	cols, rows := m.termSize()
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return m.SendResize(ctx, sessionID, cols, rows)
}

func (m *remoteManager) queueInput(sessionID string, data []byte) {
	if len(data) == 0 || sessionID == "" {
		return
	}
	m.mu.Lock()
	view := m.views[sessionID]
	if view == nil {
		view = &remoteView{id: sessionID}
		m.views[sessionID] = view
	}
	if len(view.pending)+len(data) > 32*1024 {
		view.pending = view.pending[:0]
	}
	view.pending = append(view.pending, data...)
	m.mu.Unlock()
}

func (m *remoteManager) flushPending(view *remoteView) {
	if view == nil {
		return
	}
	m.mu.Lock()
	client := view.client
	disabled := view.disabled
	awaiting := view.awaiting
	m.mu.Unlock()
	if client == nil || disabled || awaiting {
		return
	}
	m.logViewState("flush_pending", view, nil)
	go func(v *remoteView) {
		for i := 0; i < 80; i++ {
			m.mu.Lock()
			pending := append([]byte(nil), v.pending...)
			client := v.client
			m.mu.Unlock()
			if len(pending) == 0 || client == nil {
				return
			}
			if err := client.SendInput(context.Background(), pending); err == nil {
				m.mu.Lock()
				v.pending = v.pending[:0]
				m.mu.Unlock()
				return
			}
			m.clock.Sleep(50 * time.Millisecond)
		}
	}(view)
}

func (m *remoteManager) handleViewClosed(view *remoteView, runID uint64, err error) {
	if view == nil {
		return
	}
	if err != nil {
		if retryDelay, ok := retryafter.FromError(err); ok && m.gate != nil {
			m.gate.BlockFor(retryDelay)
		}
	}
	var restart bool
	var ctx context.Context
	m.mu.Lock()
	current := m.views[view.id]
	if current == view && view.runID == runID {
		view.running = false
		view.cancel = nil
		if view.sessionClosed {
			delete(m.retained, view.id)
			delete(m.views, view.id)
			delete(m.disabled, view.id)
		} else if view.restart {
			restart = true
			view.restart = false
			ctx = view.ctx
		} else if view.client != nil || view.visible || view.awaiting || view.disabled || shouldRetainRemoteView(view) {
			if view.missingSince.IsZero() {
				view.missingSince = m.clock.Now()
			}
			m.retained[view.id] = view.missingSince
			view.visible = true
			view.hiddenAt = time.Time{}
		} else {
			delete(m.retained, view.id)
			delete(m.views, view.id)
		}
	}
	m.mu.Unlock()
	m.logViewState("view_closed", view, err)
	if view.ctx != nil && view.ctx.Err() == nil && !view.sessionClosed && err != nil {
		_ = m.refreshSessions(view.ctx)
	}
	if restart && ctx != nil && ctx.Err() == nil {
		_ = m.connectView(ctx, view, io.Discard, func() {
			m.markReady(view.id)
		})
	}
	if m.onViewClosed != nil {
		m.onViewClosed(view.id, err)
	}
}

func (m *remoteManager) refreshSessions(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if m.gate != nil && !m.gate.Allowed() {
		return nil
	}
	_, httpURL, err := normalizeEndpoint(m.endpoint)
	if err != nil {
		return err
	}
	sessions, err := m.fetchSessions(ctx, httpURL)
	if err != nil {
		return err
	}
	m.applySessions(sessions)
	return nil
}

func (m *remoteManager) Refresh(ctx context.Context) error {
	return m.refreshSessions(ctx)
}

func (m *remoteManager) applySessions(sessions []remoteSessionInfo) {
	rawCount := len(sessions)
	if rawCount == 0 && len(m.sessions) > 0 {
		if m.clock.Now().Sub(m.lastNonEmpty) < 5*time.Second {
			m.logger.Debug("session.remote.sessions.empty.ignored")
			return
		}
	}
	sessions = ensureLocalSession(sessions, m.localID, m.localName, m.clock.Now().UTC())
	mvu.SortSessionsByLastActive(sessions)

	m.mu.Lock()
	now := m.clock.Now()
	if rawCount > 0 {
		m.lastNonEmpty = now
	}
	changed := sessionsKey(m.sessions) != sessionsKey(sessions)
	allIDs := make(map[string]struct{}, len(sessions))
	prevSessions := make(map[string]remoteSessionInfo, len(m.sessions))
	for _, session := range sessions {
		allIDs[session.ID] = struct{}{}
	}
	for _, session := range m.sessions {
		prevSessions[session.ID] = session
	}
	retainedSessions := make([]remoteSessionInfo, 0)
	reconnectViews := make([]*remoteView, 0)
	for id, view := range m.views {
		if _, ok := allIDs[id]; ok {
			delete(m.retained, id)
			view.missingSince = time.Time{}
			view.sessionClosed = false
			if view.visible && view.cancel == nil && !view.awaiting && !view.disabled && !view.running {
				view.awaiting = true
				reconnectViews = append(reconnectViews, view)
			}
			continue
		}
		if !shouldRetainRemoteView(view) {
			if view.cancel != nil {
				view.cancel()
			}
			delete(m.views, id)
			delete(m.disabled, id)
			delete(m.retained, id)
			continue
		}
		if view.sessionClosed {
			if view.cancel != nil {
				view.cancel()
			}
			delete(m.views, id)
			delete(m.disabled, id)
			delete(m.retained, id)
			continue
		}
		if view.missingSince.IsZero() {
			view.missingSince = now
		}
		m.retained[id] = now
		if now.Sub(view.missingSince) >= missingSessionGrace {
			if view.cancel != nil {
				view.cancel()
			}
			delete(m.views, id)
			delete(m.disabled, id)
			delete(m.retained, id)
			continue
		}
		if prev, ok := prevSessions[id]; ok {
			retainedSessions = append(retainedSessions, prev)
		} else {
			retainedSessions = append(retainedSessions, remoteSessionInfo{
				ID:           id,
				Name:         view.name,
				Status:       "reconnecting",
				LastActiveAt: now,
			})
		}
	}
	m.sessions = append(sessions, retainedSessions...)
	if len(m.sessions) > 0 {
		mvu.SortSessionsByLastActive(m.sessions)
	}
	callback := m.onSessions
	nextSessions := copySessions(m.sessions)
	m.mu.Unlock()

	for _, view := range reconnectViews {
		if view == nil || view.ctx == nil {
			continue
		}
		if err := m.connectView(view.ctx, view, view.stdout, func() {
			m.markReady(view.id)
		}); err != nil {
			m.logger.Debug("session.remote.reconnect.failed", "session", view.id, "err", err)
		}
	}

	if changed && callback != nil {
		callback(nextSessions)
	}
}

func (m *remoteManager) handleExplicitSessionClosed(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	view := m.views[sessionID]
	var cancel context.CancelFunc
	if view != nil {
		view.sessionClosed = true
		cancel = view.cancel
		delete(m.views, sessionID)
	}
	delete(m.retained, sessionID)
	delete(m.disabled, sessionID)
	nextSessions := make([]remoteSessionInfo, 0, len(m.sessions))
	for _, session := range m.sessions {
		if session.ID == sessionID {
			continue
		}
		nextSessions = append(nextSessions, session)
	}
	changed := sessionsKey(m.sessions) != sessionsKey(nextSessions)
	m.sessions = nextSessions
	callback := m.onSessions
	closeCallback := m.onViewClosed
	copied := copySessions(m.sessions)
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if changed && callback != nil {
		callback(copied)
	}
	if closeCallback != nil {
		closeCallback(sessionID, fmt.Errorf("session closed"))
	}
}

func shouldRetainRemoteView(view *remoteView) bool {
	if view == nil {
		return false
	}
	return view.visible || view.awaiting || view.running || view.client != nil || view.disabled
}

func (m *remoteManager) IsRetained(sessionID string) bool {
	if m == nil || sessionID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if retainedAt, ok := m.retained[sessionID]; ok {
		if m.clock.Now().Sub(retainedAt) < missingSessionGrace {
			return true
		}
		delete(m.retained, sessionID)
	}
	view := m.views[sessionID]
	if view == nil {
		return false
	}
	if shouldRetainRemoteView(view) {
		return true
	}
	if view.missingSince.IsZero() {
		return false
	}
	return m.clock.Now().Sub(view.missingSince) < missingSessionGrace
}

func (m *remoteManager) fetchSessions(ctx context.Context, httpURL string) ([]remoteSessionInfo, error) {
	if m.tokenRefresher != nil {
		if _, err := m.refreshTokenErr(ctx); err != nil {
			return nil, err
		}
	}
	client, err := m.sessionsHTTPClient()
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, remoteSessionsRequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, httpURL+"/sessions", nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+m.token)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && m.refreshToken(ctx) {
			_ = resp.Body.Close()
			cancel()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			cancel()
			return nil, fmt.Errorf("list sessions failed: %s", resp.Status)
		}
		var sessions []remoteSessionInfo
		if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
			cancel()
			return nil, err
		}
		cancel()
		return sessions, nil
	}
	return nil, fmt.Errorf("list sessions failed: unauthorized")
}

func (m *remoteManager) sessionsHTTPClient() (*http.Client, error) {
	m.mu.Lock()
	if m.httpClient != nil {
		client := m.httpClient
		m.mu.Unlock()
		return client, nil
	}
	m.mu.Unlock()

	tlsCfg, err := clientTLSConfig(m.tlsDir, m.insecure)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{
		Timeout:   remoteSessionsRequestTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   remoteSessionsRequestTimeout,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{Transport: transport}

	m.mu.Lock()
	if m.httpClient == nil {
		m.httpClient = client
		m.httpTr = transport
		m.mu.Unlock()
		return client, nil
	}
	existing := m.httpClient
	m.mu.Unlock()
	transport.CloseIdleConnections()
	return existing, nil
}

func (m *remoteManager) closeHTTPClient() {
	m.mu.Lock()
	transport := m.httpTr
	m.httpClient = nil
	m.httpTr = nil
	m.mu.Unlock()
	if transport != nil {
		transport.CloseIdleConnections()
	}
}

func ensureLocalSession(sessions []remoteSessionInfo, localID, localName string, now time.Time) []remoteSessionInfo {
	found := false
	for i := range sessions {
		if sessions[i].ID == localID {
			found = true
			if sessions[i].Name == "" && localName != "" {
				sessions[i].Name = localName
			}
			break
		}
	}
	if found || localID == "" {
		return sessions
	}
	return append(sessions, remoteSessionInfo{
		ID:           localID,
		Name:         localName,
		Status:       "active",
		LastActiveAt: now,
	})
}

func sessionByID(sessions []remoteSessionInfo, id string) (remoteSessionInfo, bool) {
	for _, session := range sessions {
		if session.ID == id {
			return session, true
		}
	}
	return remoteSessionInfo{}, false
}

func copySessions(sessions []remoteSessionInfo) []remoteSessionInfo {
	if len(sessions) == 0 {
		return nil
	}
	out := make([]remoteSessionInfo, len(sessions))
	copy(out, sessions)
	return out
}

func toRemoteSessions(sessions []attach.SessionInfo) []remoteSessionInfo {
	if len(sessions) == 0 {
		return nil
	}
	out := make([]remoteSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, remoteSessionInfo{
			ID:           session.ID,
			Name:         session.Name,
			Headless:     session.Headless,
			Status:       session.Status,
			LastActiveAt: session.LastActiveAt,
		})
	}
	mvu.SortSessionsByLastActive(out)
	return out
}

func toRemoteSessionsFromProto(infos []*protocolpb.SessionInfo) []remoteSessionInfo {
	if len(infos) == 0 {
		return nil
	}
	out := make([]remoteSessionInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		out = append(out, remoteSessionInfo{
			ID:           info.Id,
			Name:         info.Name,
			Headless:     info.Headless,
			Status:       info.Status,
			LastActiveAt: time.Unix(info.LastActiveUnix, 0).UTC(),
		})
	}
	mvu.SortSessionsByLastActive(out)
	return out
}

func sessionsKey(sessions []remoteSessionInfo) string {
	var b strings.Builder
	for _, session := range sessions {
		b.WriteString(session.ID)
		b.WriteByte('|')
		b.WriteString(session.Name)
		b.WriteByte('|')
	}
	return b.String()
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
