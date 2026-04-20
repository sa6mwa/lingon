package ptytest

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/lingon/internal/trace"
)

// Harness provisions an in-process server and PTY-backed clients.
type Harness struct {
	t *testing.T

	home        string
	basePath    string
	addr        string
	endpoint    string
	accessToken string
	refresh     relay.RefreshToken
	tlsConfig   *tls.Config
	auth        *relay.Authenticator
	usersPath   string
	dataDir     string
	authPath    string

	users        *relay.UserStore
	store        *relay.Store
	hub          *relay.Hub
	recorder     *WSRecorder
	onRequest    func(*http.Request)
	connectLimit *relay.ConnectLimitConfig
	wallTimeout  time.Duration
	wallLevels   []time.Duration
	clock        clock.Clock
	tracePath    string
	trace        *trace.Writer

	server *httptest.Server
	proxy  *httptest.Server
	ctx    context.Context
	cancel context.CancelFunc
}

// UserAuth describes credentials created by the harness.
type UserAuth struct {
	Username    string
	Password    string
	TOTPSecret  string
	AccessToken string
}

// HarnessOption configures a PTY test harness.
type HarnessOption func(*Harness)

// WithBasePath sets the API base path used by the harness.
func WithBasePath(path string) HarnessOption {
	return func(h *Harness) {
		h.basePath = path
	}
}

// WithWSRecorder enables a websocket proxy recorder for frame-level assertions.
func WithWSRecorder(recorder *WSRecorder) HarnessOption {
	return func(h *Harness) {
		h.recorder = recorder
	}
}

// WithRequestHook installs a callback for each HTTP request handled by the harness server.
func WithRequestHook(fn func(*http.Request)) HarnessOption {
	return func(h *Harness) {
		h.onRequest = fn
	}
}

// WithConnectLimiter configures the relay connect limiter for the harness server.
func WithConnectLimiter(cfg relay.ConnectLimitConfig) HarnessOption {
	return func(h *Harness) {
		h.connectLimit = &cfg
	}
}

// WithWallConfig sets relay wall timeout and inactivity levels for the harness server.
func WithWallConfig(timeout time.Duration, inactiveAfterLevels []time.Duration) HarnessOption {
	return func(h *Harness) {
		h.wallTimeout = timeout
		if len(inactiveAfterLevels) == 0 {
			h.wallLevels = nil
			return
		}
		h.wallLevels = append([]time.Duration(nil), inactiveAfterLevels...)
	}
}

// WithClock sets the harness clock used by sessions.
func WithClock(clk clock.Clock) HarnessOption {
	return func(h *Harness) {
		h.clock = clk
	}
}

// WithTracePath enables a shared trace file for sessions started by the harness.
func WithTracePath(path string) HarnessOption {
	return func(h *Harness) {
		h.tracePath = path
	}
}

// New creates a PTY test harness with an in-process server.
func New(t *testing.T, opts ...HarnessOption) *Harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &Harness{
		t:        t,
		basePath: "/v1",
		ctx:      ctx,
		cancel:   cancel,
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.clock == nil {
		h.clock = clock.New()
	}
	if h.tracePath != "" {
		tw, err := trace.New(h.tracePath)
		if err != nil {
			t.Fatalf("trace.New: %v", err)
		}
		h.trace = tw
	}
	h.initServer()
	t.Cleanup(h.Close)
	return h
}

// Clock returns the harness clock.
func (h *Harness) Clock() clock.Clock {
	return h.clock
}

// TracePath returns the trace file path for the harness, if enabled.
func (h *Harness) TracePath() string {
	return h.tracePath
}

// Advance moves harness time forward for mock clocks or sleeps for real clocks.
func (h *Harness) Advance(d time.Duration) {
	Advance(h.clock, d)
}

// Endpoint returns the HTTPS base endpoint for the harness server.
func (h *Harness) Endpoint() string {
	return h.endpoint
}

// AccessToken returns the seeded access token for the harness user.
func (h *Harness) AccessToken() string {
	return h.accessToken
}

// RefreshToken returns the seeded refresh token for the harness user.
func (h *Harness) RefreshToken() relay.RefreshToken {
	return h.refresh
}

// CreateUserWithToken creates a user and returns its access token.
func (h *Harness) CreateUserWithToken(username, password string) UserAuth {
	h.t.Helper()
	if h.users == nil || h.store == nil {
		h.t.Fatalf("harness not initialized")
	}
	result, err := relay.CreateUser(h.users, username, password, time.Now().UTC())
	if err != nil {
		h.t.Fatalf("CreateUser: %v", err)
	}
	if err := h.users.Save(h.usersPath); err != nil {
		h.t.Fatalf("Save users: %v", err)
	}
	access, err := h.store.CreateAccessToken(result.User.Username, relay.DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		h.t.Fatalf("CreateAccessToken: %v", err)
	}
	return UserAuth{
		Username:    result.User.Username,
		Password:    result.Password,
		TOTPSecret:  result.TOTPSecret,
		AccessToken: access.Token,
	}
}

// CreateShareToken creates a share token for a session.
func (h *Harness) CreateShareToken(sessionID string, scope relay.ShareScope, ttl time.Duration) string {
	h.t.Helper()
	if h.store == nil {
		h.t.Fatalf("harness not initialized")
	}
	share, err := h.store.CreateShareToken(sessionID, scope, ttl, time.Now().UTC())
	if err != nil {
		h.t.Fatalf("CreateShareToken: %v", err)
	}
	return share.Token
}

// Close stops the harness server.
func (h *Harness) Close() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.hub != nil {
		h.hub.CloseAll("shutdown")
	}
	if h.proxy != nil {
		h.proxy.CloseClientConnections()
		h.proxy.Close()
	}
	if h.server != nil {
		h.server.CloseClientConnections()
		h.server.Close()
	}
	if h.trace != nil {
		_ = h.trace.Close()
	}
}

// ClientCount reports how many relay clients are connected for a session.
func (h *Harness) ClientCount(sessionID string) int {
	if h.hub == nil {
		return 0
	}
	return h.hub.ClientCount(sessionID)
}

// HasHost reports whether a host connection is registered for the session.
func (h *Harness) HasHost(sessionID string) bool {
	if h.hub == nil {
		return false
	}
	return h.hub.HasHost(sessionID)
}

// SessionSeq returns the relay sequence number for a session, if available.
func (h *Harness) SessionSeq(sessionID string) uint64 {
	if h.hub == nil {
		return 0
	}
	return h.hub.Seq(sessionID)
}

// StopServer shuts down the harness server without restarting it.
func (h *Harness) StopServer() {
	h.t.Helper()
	if h.hub != nil {
		h.hub.CloseAll("shutdown")
	}
	if h.proxy != nil && h.recorder == nil {
		h.proxy.CloseClientConnections()
		h.proxy.Close()
		h.proxy = nil
	}
	if h.server != nil {
		h.server.CloseClientConnections()
		h.server.Close()
		h.server = nil
	}
}

func (h *Harness) initServer() {
	h.home = testutil.TempDir(h.t)
	h.t.Setenv("HOME", h.home)

	tlsDir := filepath.Join(h.home, ".lingon", "tls")
	populateTLSDir(h.t, tlsDir)
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		h.t.Fatalf("LoadLocalServerCert: %v", err)
	}
	h.tlsConfig = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	usersPath := filepath.Join(h.home, ".lingon", "users.json")
	h.usersPath = usersPath
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		h.t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		h.t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	now := time.Now().UTC()
	refresh, err := store.CreateRefreshTokenForClient("test", "cli", time.Hour, now)
	if err != nil {
		h.t.Fatalf("CreateRefreshToken: %v", err)
	}
	access, err := store.CreateAccessTokenForRefresh("test", refresh.Token, "cli", time.Minute, now)
	if err != nil {
		h.t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	h.auth = auth

	h.dataDir = filepath.Join(h.home, ".lingon")
	h.users = users
	h.store = store

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatalf("listen: %v", err)
	}
	h.addr = listener.Addr().String()
	h.startServer(listener)

	h.endpoint = "https://" + h.addr + h.basePath
	if h.recorder != nil {
		h.startProxy()
	}
	h.accessToken = access.Token
	h.refresh = refresh
	h.authPath = filepath.Join(h.home, ".lingon", "auth.json")
	state := authstore.State{
		Endpoint:         h.endpoint,
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	}
	if err := authstore.Save(h.authPath, state); err != nil {
		h.t.Fatalf("save auth state: %v", err)
	}
}

// RestartServer stops the current server and restarts it on the same address.
func (h *Harness) RestartServer() {
	h.t.Helper()
	if h.server != nil {
		if h.hub != nil {
			h.hub.CloseAll("shutdown")
		}
		h.server.CloseClientConnections()
		h.server.Close()
		h.server = nil
	}
	if h.proxy != nil && h.recorder == nil {
		h.proxy.CloseClientConnections()
		h.proxy.Close()
		h.proxy = nil
	}
	var listener net.Listener
	var err error
	for i := 0; i < 10; i++ {
		listener, err = net.Listen("tcp", h.addr)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if listener == nil {
		h.t.Fatalf("restart listen: %v", err)
	}
	h.startServer(listener)
	if h.recorder != nil && h.proxy == nil {
		h.startProxy()
	}
}

func (h *Harness) startServer(listener net.Listener) {
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(h.store, h.users, h.auth, nil, hub)
	relayServer.UsersFile = h.usersPath
	relayServer.DataDir = h.dataDir
	if h.wallTimeout > 0 || len(h.wallLevels) > 0 {
		relayServer.ConfigureWall(h.wallTimeout, h.wallLevels)
	}
	if h.connectLimit != nil {
		relayServer.ConnectLimiter = relay.NewConnectLimiter(*h.connectLimit)
	}

	handler := relayServer.Handler()
	if h.onRequest != nil {
		next := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.onRequest(r)
			next.ServeHTTP(w, r)
		})
	}
	handler = server.WrapBasePath(h.basePath, handler)
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = h.tlsConfig
	srv.Listener = listener
	srv.StartTLS()

	h.server = srv
	h.hub = hub
}

func (h *Harness) startProxy() {
	h.t.Helper()
	upstream := "https://" + h.addr
	handler, err := newWSProxy(upstream, h.basePath, h.recorder)
	if err != nil {
		h.t.Fatalf("proxy setup: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = h.tlsConfig
	srv.StartTLS()
	h.proxy = srv
	h.endpoint = srv.URL + h.basePath
}

// HostOptions configures a PTY-backed host session.
type HostOptions struct {
	SessionID                   string
	SessionName                 string
	Shell                       string
	Cols                        int
	Rows                        int
	Clock                       clock.Clock
	OnPTYRead                   func([]byte)
	DisableRaw                  bool
	DisablePublish              bool
	DisableDesktopNotifications bool
	DesktopNotifier             desktopnotify.Notifier
	// AccessToken overrides the harness default access token.
	AccessToken string
	// AuthFile is the path to the auth state file used for refresh.
	AuthFile string
}

type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, desktopnotify.Request) error {
	return nil
}

func effectiveHostDesktopNotificationConfig(opts HostOptions) (bool, desktopnotify.Notifier) {
	if opts.DisableDesktopNotifications {
		return true, opts.DesktopNotifier
	}
	if opts.DesktopNotifier != nil {
		return false, opts.DesktopNotifier
	}
	return false, noopNotifier{}
}

func effectiveAttachDesktopNotificationConfig(disabled bool, notifier desktopnotify.Notifier) (bool, desktopnotify.Notifier) {
	if disabled {
		return true, notifier
	}
	if notifier != nil {
		return false, notifier
	}
	return false, noopNotifier{}
}

// StartHost launches a host session attached to a PTY.
func (h *Harness) StartHost(opts HostOptions) *PTYSession {
	h.t.Helper()
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	clk := opts.Clock
	if clk == nil {
		clk = h.clock
	}
	if opts.Shell == "" {
		opts.Shell = "/bin/sh"
	}
	if opts.SessionID == "" {
		opts.SessionID = "host"
	}
	if opts.SessionName == "" {
		opts.SessionName = opts.SessionID
	}
	master, slave, err := pty.Open()
	if err != nil {
		h.t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)}); err != nil {
		h.t.Fatalf("pty.Setsize: %v", err)
	}
	emu := emu.New(opts.Cols, opts.Rows)
	sess := newPTYSession(h.t, master, slave, emu)
	sess.clock = clk

	token := opts.AccessToken
	if token == "" {
		token = h.accessToken
	}
	authFile := opts.AuthFile
	if authFile == "" && opts.AccessToken == "" {
		authFile = h.authPath
	}
	disableDesktopNotifications, desktopNotifier := effectiveHostDesktopNotificationConfig(opts)
	resizeCh := make(chan struct{}, 1)
	runner := session.New(session.Options{
		Endpoint:                    h.endpoint,
		Token:                       token,
		AuthFile:                    authFile,
		SessionID:                   opts.SessionID,
		SessionName:                 opts.SessionName,
		Cols:                        opts.Cols,
		Rows:                        opts.Rows,
		Shell:                       opts.Shell,
		Publish:                     !opts.DisablePublish,
		Stdin:                       slave,
		Stdout:                      slave,
		DisableRaw:                  opts.DisableRaw,
		Clock:                       clk,
		OnPTYRead:                   opts.OnPTYRead,
		DisableDesktopNotifications: disableDesktopNotifications,
		DesktopNotifier:             desktopNotifier,
		Trace:                       h.trace,
		ResizeEvents:                resizeCh,
		DisableSignalResize:         true,
	})

	go func() {
		sess.runErr <- runner.Run(sess.ctx)
	}()

	sess.cleanup = func() {
		_ = master.Close()
		_ = slave.Close()
	}
	sess.onResize = func() {
		select {
		case resizeCh <- struct{}{}:
		default:
		}
	}

	return sess
}

// AttachOptions configures a PTY-backed attach client.
type AttachOptions struct {
	SessionID      string
	ClientID       string
	RequestControl bool
	Cols           int
	Rows           int
	Clock          clock.Clock
	// Endpoint overrides the harness relay endpoint. Supports local headless endpoints in tests.
	Endpoint string
	// UnixSocket overrides the transport socket used by attach client tests.
	UnixSocket string
	// DisableDesktopNotifications suppresses desktop notifications in the attach client.
	DisableDesktopNotifications bool
	// DesktopNotifier overrides the attach client's notifier.
	DesktopNotifier desktopnotify.Notifier
	// NoHostTimeout controls how long to wait for a host before failing.
	NoHostTimeout time.Duration
	// AccessToken overrides the harness default access token.
	AccessToken string
	// AuthFile is the path to the auth state file used for refresh.
	AuthFile string
	// ShareToken uses a share token instead of access token auth.
	ShareToken string
	// RawInput configures the PTY slave in raw mode for input handling.
	RawInput bool
}

// StartAttach launches an attach client attached to a PTY.
func (h *Harness) StartAttach(opts AttachOptions) *PTYSession {
	h.t.Helper()
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	if opts.SessionID == "" {
		h.t.Fatalf("attach session id is required")
	}
	clk := opts.Clock
	if clk == nil {
		clk = h.clock
	}

	master, slave, err := pty.Open()
	if err != nil {
		h.t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)}); err != nil {
		h.t.Fatalf("pty.Setsize: %v", err)
	}
	var stdinState *term.State
	if opts.RawInput {
		state, err := term.MakeRaw(int(slave.Fd()))
		if err != nil {
			h.t.Fatalf("term.MakeRaw: %v", err)
		}
		stdinState = state
	}

	emu := emu.New(opts.Cols, opts.Rows)
	sess := newPTYSession(h.t, master, slave, emu)
	sess.clock = clk
	size := &sizeProvider{cols: opts.Cols, rows: opts.Rows}
	sess.size = size
	resizeCh := make(chan struct{}, 1)

	clientID := opts.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("attach-%s", opts.SessionID)
	}

	token := opts.AccessToken
	if token == "" {
		token = h.accessToken
	}
	authFile := opts.AuthFile
	if authFile == "" && opts.AccessToken == "" {
		authFile = h.authPath
	}
	disableDesktopNotifications, desktopNotifier := effectiveAttachDesktopNotificationConfig(
		opts.DisableDesktopNotifications,
		opts.DesktopNotifier,
	)
	client := &attach.Client{
		Endpoint:                    h.endpoint,
		SessionID:                   opts.SessionID,
		AccessToken:                 token,
		ShareToken:                  opts.ShareToken,
		RequestControl:              opts.RequestControl,
		ClientID:                    clientID,
		DisableDesktopNotifications: disableDesktopNotifications,
		DesktopNotifier:             desktopNotifier,
		Stdin:                       slave,
		Stdout:                      slave,
		Stderr:                      io.Discard,
		TermSize:                    size.Size,
		ResizeEvents:                resizeCh,
		DisableSignalResize:         true,
		Clock:                       clk,
		NoHostTimeout:               opts.NoHostTimeout,
		UnixSocket:                  opts.UnixSocket,
	}
	if endpoint := strings.TrimSpace(opts.Endpoint); endpoint != "" {
		client.Endpoint = endpoint
	}
	if authFile != "" {
		client.TokenRefresher = relayclient.TokenRefresher(h.endpoint, authFile, "", false, func(token string) {
			client.AccessToken = token
		})
	}

	go func() {
		sess.runErr <- client.Run(sess.ctx)
	}()

	sess.cleanup = func() {
		if stdinState != nil {
			_ = term.Restore(int(slave.Fd()), stdinState)
		}
		_ = master.Close()
		_ = slave.Close()
	}
	sess.onResize = func() {
		select {
		case resizeCh <- struct{}{}:
		default:
		}
	}

	return sess
}

// MultiAttachOptions configures a PTY-backed multi-session attach client.
type MultiAttachOptions struct {
	SessionID string
	Cols      int
	Rows      int
	// DisableDesktopNotifications suppresses desktop notifications in child attach views.
	DisableDesktopNotifications bool
	// DesktopNotifier overrides the notifier used by child attach views.
	DesktopNotifier desktopnotify.Notifier
	// Endpoint overrides the harness relay endpoint.
	Endpoint string
	// AccessToken overrides the harness default access token.
	AccessToken string
	// AuthFile is the path to the auth state file used for refresh.
	AuthFile string
	// AllowOfflineToggle forwards Ctrl+L o to local host transports.
	AllowOfflineToggle bool
	// SessionSource overrides relay /sessions with a custom local source.
	SessionSource func(context.Context) ([]attach.SessionInfo, error)
	// SocketResolver maps session id to a unix domain socket path.
	SocketResolver func(sessionID string) (string, error)
	// SessionEvents triggers immediate local session-list refreshes.
	SessionEvents <-chan struct{}
	// Clock controls attach time in tests.
	Clock clock.Clock
	// OnView is called when a session view is created.
	OnView func(sessionID string, client *attach.Client)
	// OnReconnect is called before a view attempts to reconnect.
	OnReconnect func(sessionID string, attempt int)
	// OnViewClosed is called when a view disconnects.
	OnViewClosed func(sessionID string, visible bool, current bool)
	// OnActive is called when a view becomes active.
	OnActive func(sessionID string)
	// BackoffPolicy overrides reconnect backoff in tests.
	BackoffPolicy backoff.Policy
	// InactiveTTL overrides the default inactive tab timeout.
	InactiveTTL time.Duration
	// RefreshInterval overrides the default session refresh interval.
	RefreshInterval time.Duration
	// RequestControl overrides the default control request behavior.
	// Nil means default true for test backward compatibility.
	RequestControl *bool
}

// StartMultiAttach launches a multi-session attach client attached to a PTY.
func (h *Harness) StartMultiAttach(opts MultiAttachOptions) *PTYSession {
	h.t.Helper()
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	clk := opts.Clock
	if clk == nil {
		clk = h.clock
	}

	master, slave, err := pty.Open()
	if err != nil {
		h.t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)}); err != nil {
		h.t.Fatalf("pty.Setsize: %v", err)
	}

	emu := emu.New(opts.Cols, opts.Rows)
	sess := newPTYSession(h.t, master, slave, emu)
	sess.clock = clk
	size := &sizeProvider{cols: opts.Cols, rows: opts.Rows}
	sess.size = size
	resizeCh := make(chan struct{}, 1)

	token := opts.AccessToken
	if token == "" {
		token = h.accessToken
	}
	if opts.AuthFile == "" && opts.AccessToken == "" && opts.SessionSource == nil {
		opts.AuthFile = h.authPath
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = h.endpoint
	}
	disableDesktopNotifications, desktopNotifier := effectiveAttachDesktopNotificationConfig(
		opts.DisableDesktopNotifications,
		opts.DesktopNotifier,
	)
	client := &attach.MultiClient{
		Endpoint:                    endpoint,
		AccessToken:                 token,
		SessionID:                   opts.SessionID,
		DisableDesktopNotifications: disableDesktopNotifications,
		DesktopNotifier:             desktopNotifier,
		Stdin:                       slave,
		Stdout:                      slave,
		Stderr:                      io.Discard,
		TermSize:                    size.Size,
		ResizeEvents:                resizeCh,
		DisableSignalResize:         true,
		AuthFile:                    opts.AuthFile,
		AllowOfflineToggle:          opts.AllowOfflineToggle,
		SessionSource:               opts.SessionSource,
		SocketResolver:              opts.SocketResolver,
		SessionEvents:               opts.SessionEvents,
		Clock:                       clk,
		OnView:                      opts.OnView,
		OnReconnect:                 opts.OnReconnect,
		OnViewClosed:                opts.OnViewClosed,
		OnActive:                    opts.OnActive,
		BackoffPolicy:               opts.BackoffPolicy,
		InactiveTTL:                 opts.InactiveTTL,
		RefreshInterval:             opts.RefreshInterval,
	}
	requestControl := true
	if opts.RequestControl != nil {
		requestControl = *opts.RequestControl
	}
	client.RequestControl = requestControl

	go func() {
		sess.runErr <- client.Run(sess.ctx)
	}()

	sess.cleanup = func() {
		_ = master.Close()
		_ = slave.Close()
	}
	sess.onResize = func() {
		select {
		case resizeCh <- struct{}{}:
		default:
		}
	}

	return sess
}

// AuthFile returns the auth state file path used by the harness.
func (h *Harness) AuthFile() string {
	return h.authPath
}

type sizeProvider struct {
	mu   sync.RWMutex
	cols int
	rows int
}

func (s *sizeProvider) Size() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cols, s.rows
}

func (s *sizeProvider) Set(cols, rows int) {
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	s.mu.Unlock()
}
