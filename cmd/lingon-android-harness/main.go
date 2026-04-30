package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/cliwall"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

type harnessConfig struct {
	Endpoint    string        `json:"endpoint"`
	CACertPath  string        `json:"ca_cert_path"`
	HostEchoLog string        `json:"host_echo_log"`
	HostCols    int           `json:"host_cols"`
	HostRows    int           `json:"host_rows"`
	Users       []harnessUser `json:"users"`
	GeneratedAt string        `json:"generated_at"`
}

type harnessUser struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	TOTPSecret string   `json:"totp_secret"`
	Sessions   []string `json:"sessions"`
	ViewToken  string   `json:"view_token"`
}

type harness struct {
	config     harnessConfig
	server     *httptest.Server
	sessions   []sessionHandle
	sessionsMu sync.Mutex
	ctx        context.Context
	baseDir    string
	configDir  string
	selfPath   string
	endpoint   string
	access     string
	authPath   string
	hostIndex  int
	headlessIx int
	cols       int
	rows       int
}

type harnessWallRequest struct {
	Message string `json:"message"`
}

type harnessWallInactivityRequest struct {
	Sessions []string `json:"sessions"`
	Enabled  bool     `json:"enabled"`
}

type harnessHeadlessRequest struct {
	SessionID string `json:"session_id"`
}

type harnessHeadlessResponse struct {
	ID string `json:"id"`
}

type harnessHeadlessSizeResponse struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type sessionHandle struct {
	id   string
	stop func()
}

func main() {
	var (
		sessionCount = flag.Int("sessions", 2, "number of host sessions to start")
		port         = flag.Int("port", 0, "port to bind (0 for random)")
		basePath     = flag.String("base-path", "/v1", "API base path")
		user         = flag.String("user", "test", "login username")
		pass         = flag.String("pass", "pass", "login password")
		cols         = flag.Int("cols", 80, "terminal columns")
		rows         = flag.Int("rows", 24, "terminal rows")
		configPath   = flag.String("config", "", "write JSON config to this path")
		hostEcho     = flag.Bool("host-echo", false, "run the host echo loop instead of the harness")
		hostID       = flag.String("host-id", "", "host echo session id")
	)
	flag.Parse()

	if *hostEcho {
		runHostEcho(*hostID)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h, err := startHarness(ctx, harnessOptions{
		sessionCount: *sessionCount,
		port:         *port,
		basePath:     *basePath,
		username:     *user,
		password:     *pass,
		cols:         *cols,
		rows:         *rows,
	})
	if err != nil {
		log.Fatalf("harness start failed: %v", err)
	}
	defer h.stop()

	if *configPath != "" {
		if err := writeConfig(*configPath, h.config); err != nil {
			log.Fatalf("write config: %v", err)
		}
	}
	printConfig(h.config)

	<-ctx.Done()
}

type harnessOptions struct {
	sessionCount int
	port         int
	basePath     string
	username     string
	password     string
	cols         int
	rows         int
}

func startHarness(ctx context.Context, opts harnessOptions) (*harness, error) {
	if os.Getenv("LINGON_DEBUG_INPUT") == "" {
		_ = os.Setenv("LINGON_DEBUG_INPUT", "1")
	}
	if os.Getenv("LINGON_HOST_PUBLISHER_PING_INTERVAL") == "" {
		_ = os.Setenv("LINGON_HOST_PUBLISHER_PING_INTERVAL", "5s")
	}
	if os.Getenv("LINGON_HOST_PUBLISHER_PING_TIMEOUT") == "" {
		_ = os.Setenv("LINGON_HOST_PUBLISHER_PING_TIMEOUT", "10s")
	}
	root, err := os.MkdirTemp("", "lingon-android-harness-")
	if err != nil {
		return nil, err
	}
	cleanupRoot := true
	defer func() {
		if cleanupRoot {
			_ = os.RemoveAll(root)
		}
	}()
	hostEchoLog := os.Getenv("LINGON_HOST_ECHO_LOG")
	if hostEchoLog == "" {
		hostEchoLog = filepath.Join(root, "host-echo.log")
		_ = os.Setenv("LINGON_HOST_ECHO_LOG", hostEchoLog)
	}
	configDir := filepath.Join(root, "config")
	tlsDir := filepath.Join(configDir, "tls")
	authPath := filepath.Join(configDir, "auth.json")
	usersPath := filepath.Join(configDir, "users.json")
	for key, value := range map[string]string{
		"LINGON_CONFIG_DIR":                             configDir,
		"LINGON_TERMINAL_DISABLE_DESKTOP_NOTIFICATIONS": "true",
	} {
		if err := os.Setenv(key, value); err != nil {
			return nil, err
		}
	}
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		return nil, err
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		return nil, err
	}

	users := relay.NewUserStore()
	userResult, err := relay.CreateUser(users, opts.username, opts.password, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	secondUser := fmt.Sprintf("%s2", opts.username)
	secondPass := fmt.Sprintf("%s2", opts.password)
	secondResult, err := relay.CreateUser(users, secondUser, secondPass, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := users.Save(usersPath); err != nil {
		return nil, err
	}

	store := relay.NewStore()
	now := time.Now().UTC()
	access, err := store.CreateAccessToken(opts.username, 24*time.Hour, now)
	if err != nil {
		return nil, err
	}
	access2, err := store.CreateAccessToken(secondUser, 24*time.Hour, now)
	if err != nil {
		return nil, err
	}

	selfPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen(listenNetwork(), listenAddr(opts.port))
	if err != nil {
		return nil, err
	}
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, err
	}
	endpoint := "https://127.0.0.1:" + portStr + ensureBasePath(opts.basePath)

	hub := relay.NewHub(nil)
	auth := relay.NewAuthenticator(users)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.ConfigureWall(harnessWallTimeout(), harnessWallLevels())
	relayServer.UsersFile = usersPath
	relayServer.DataDir = configDir
	h := &harness{
		config: harnessConfig{
			Endpoint:    endpoint,
			CACertPath:  filepath.Join(tlsDir, "ca.pem"),
			HostEchoLog: hostEchoLog,
			HostCols:    opts.cols,
			HostRows:    opts.rows,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		ctx:       ctx,
		baseDir:   root,
		configDir: configDir,
		selfPath:  selfPath,
		endpoint:  endpoint,
		access:    access.Token,
		authPath:  authPath,
		hostIndex: 0,
		cols:      opts.cols,
		rows:      opts.rows,
	}
	if err := lingon.SaveAuth(h.authPath, lingon.AuthState{
		Endpoint:        endpoint,
		AccessToken:     access.Token,
		AccessExpiresAt: access.ExpiresAt,
	}); err != nil {
		return nil, err
	}

	controlPath := ensureBasePath(opts.basePath) + "/__harness/start-host"
	wallPath := ensureBasePath(opts.basePath) + "/__harness/send-wall"
	wallInactivityPath := ensureBasePath(opts.basePath) + "/__harness/wall-inactivity"
	startHeadlessPath := ensureBasePath(opts.basePath) + "/__harness/start-headless"
	detachHeadlessPath := ensureBasePath(opts.basePath) + "/__harness/detach-headless"
	headlessSizePath := ensureBasePath(opts.basePath) + "/__harness/headless-size"
	relayHandler := server.WrapBasePath(opts.basePath, relayServer.Handler())
	clientDelay := time.Duration(0)
	if raw := strings.TrimSpace(os.Getenv("LINGON_ANDROID_HARNESS_CLIENT_DELAY_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			clientDelay = time.Duration(ms) * time.Millisecond
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case controlPath:
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			count := 1
			if raw := r.URL.Query().Get("count"); raw != "" {
				if value, err := strconv.Atoi(raw); err == nil && value > 0 {
					count = value
				}
			}
			ids := make([]string, 0, count)
			for i := 0; i < count; i++ {
				id, err := h.startHost(h.access)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				ids = append(ids, id)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ids": ids})
			return
		case wallPath:
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req harnessWallRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if err := h.sendWallCLI(strings.TrimSpace(req.Message)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "sent"})
			return
		case wallInactivityPath:
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req harnessWallInactivityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if err := h.setWallInactivity(req.Sessions, req.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "updated"})
			return
		case startHeadlessPath:
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			id, err := h.startHeadless()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(harnessHeadlessResponse{ID: id})
			return
		case detachHeadlessPath:
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req harnessHeadlessRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if err := headless.DetachSession(r.Context(), h.configDir, strings.TrimSpace(req.SessionID)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "detached"})
			return
		case headlessSizePath:
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
			cols, rows, err := h.readHeadlessSize(r.Context(), sessionID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(harnessHeadlessSizeResponse{Cols: cols, Rows: rows})
			return
		default:
			if clientDelay > 0 && strings.HasSuffix(r.URL.Path, "/ws/client") {
				time.Sleep(clientDelay)
			}
			relayHandler.ServeHTTP(w, r)
			return
		}
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.Listener = listener
	srv.StartTLS()
	h.server = srv

	user1 := harnessUser{
		Username:   userResult.User.Username,
		Password:   userResult.Password,
		TOTPSecret: userResult.TOTPSecret,
	}
	user2 := harnessUser{
		Username:   secondResult.User.Username,
		Password:   secondResult.Password,
		TOTPSecret: secondResult.TOTPSecret,
	}

	if opts.sessionCount > 0 {
		for i := 1; i <= opts.sessionCount; i++ {
			id, err := h.startHost(access.Token)
			if err != nil {
				h.stop()
				return nil, err
			}
			user1.Sessions = append(user1.Sessions, id)
		}

		secondID := fmt.Sprintf("host-%d", opts.sessionCount+1)
		scriptPath, err := writeHostScript(root, secondID, selfPath)
		if err != nil {
			h.stop()
			return nil, err
		}
		cancel, err := startHostSession(ctx, endpoint, access2.Token, secondID, scriptPath, opts.cols, opts.rows, tlsDir)
		if err != nil {
			h.stop()
			return nil, err
		}
		h.sessions = append(h.sessions, sessionHandle{id: secondID, stop: cancel})
		user2.Sessions = append(user2.Sessions, secondID)
		h.hostIndex = opts.sessionCount + 1
	}

	if len(user1.Sessions) > 0 {
		share, err := store.CreateShareToken(user1.Sessions[0], relay.ShareScopeView, 24*time.Hour, time.Now().UTC())
		if err != nil {
			h.stop()
			return nil, err
		}
		user1.ViewToken = share.Token
	}
	if len(user2.Sessions) > 0 {
		share, err := store.CreateShareToken(user2.Sessions[0], relay.ShareScopeView, 24*time.Hour, time.Now().UTC())
		if err != nil {
			h.stop()
			return nil, err
		}
		user2.ViewToken = share.Token
	}

	h.config.Users = []harnessUser{user1, user2}

	cleanupRoot = false
	return h, nil
}

func (h *harness) sendWallCLI(message string) error {
	if message == "" {
		return fmt.Errorf("message is required")
	}
	return cliwall.Execute(context.Background(), cliwall.Request{
		Loader:          lingon.NewLoader(),
		Endpoint:        h.endpoint,
		EndpointChanged: true,
		AuthFile:        h.authPath,
		AuthFileChanged: true,
		Message:         message,
		Insecure:        true,
		Quiet:           true,
		Stdout:          io.Discard,
	})
}

func (h *harness) setWallInactivity(sessions []string, enabled bool) error {
	if len(sessions) == 0 {
		return fmt.Errorf("sessions are required")
	}
	for _, sessionID := range sessions {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, err := relayclient.SetWallInactivity(
			context.Background(),
			h.endpoint,
			h.access,
			sessionID,
			enabled,
			filepath.Join(h.configDir, "tls"),
			true,
		); err != nil {
			return fmt.Errorf("set wall inactivity for %s: %w", sessionID, err)
		}
	}
	return nil
}

func harnessWallTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("LINGON_ANDROID_HARNESS_WALL_TIMEOUT"))
	if raw == "" {
		return time.Second
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return time.Second
	}
	return parsed
}

func harnessWallLevels() []time.Duration {
	raw := strings.TrimSpace(os.Getenv("LINGON_ANDROID_HARNESS_WALL_LEVELS"))
	if raw == "" {
		return []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}
	}
	parts := strings.Split(raw, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := time.ParseDuration(part)
		if err != nil || parsed <= 0 {
			continue
		}
		out = append(out, parsed)
	}
	if len(out) == 0 {
		return []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}
	}
	return out
}

func (h *harness) stop() {
	h.sessionsMu.Lock()
	for _, sess := range h.sessions {
		if sess.stop != nil {
			sess.stop()
		}
	}
	h.sessionsMu.Unlock()
	if h.server != nil {
		h.server.CloseClientConnections()
		h.server.Close()
	}
	if h.baseDir != "" {
		_ = os.RemoveAll(h.baseDir)
		h.baseDir = ""
	}
}

func (h *harness) startHost(token string) (string, error) {
	if h.ctx.Err() != nil {
		return "", h.ctx.Err()
	}
	h.hostIndex++
	id := fmt.Sprintf("host-%d", h.hostIndex)
	scriptPath, err := writeHostScript(h.baseDir, id, h.selfPath)
	if err != nil {
		return "", err
	}
	cancel, err := startHostSession(h.ctx, h.endpoint, token, id, scriptPath, h.cols, h.rows, filepath.Join(h.configDir, "tls"))
	if err != nil {
		return "", err
	}
	h.sessionsMu.Lock()
	h.sessions = append(h.sessions, sessionHandle{id: id, stop: cancel})
	h.sessionsMu.Unlock()
	return id, nil
}

func (h *harness) startHeadless() (string, error) {
	if h.ctx.Err() != nil {
		return "", h.ctx.Err()
	}
	h.headlessIx++
	id := fmt.Sprintf("headless-%d", h.headlessIx)
	runCtx, cancelRun := context.WithCancel(h.ctx)
	started := false
	defer func() {
		if !started {
			cancelRun()
		}
	}()
	daemon := headlessd.New(headlessd.Options{
		ConfigDir:      h.configDir,
		Endpoint:       h.endpoint,
		Token:          h.access,
		AuthFile:       h.authPath,
		SessionID:      id,
		Cols:           120,
		Rows:           50,
		Shell:          defaultHeadlessShell(),
		Publish:        true,
		PublishControl: true,
		Logger:         pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- daemon.Run(runCtx)
	}()
	socketPath, err := headless.SocketPath(h.configDir, id)
	if err != nil {
		cancelRun()
		return "", err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-runErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				return "", err
			}
			return "", fmt.Errorf("headless daemon %s exited before socket ready", id)
		default:
		}
		if headless.SocketExists(socketPath) {
			h.sessionsMu.Lock()
			h.sessions = append(h.sessions, sessionHandle{
				id: id,
				stop: func() {
					cancelRun()
					select {
					case <-runErr:
					case <-time.After(3 * time.Second):
					}
				},
			})
			h.sessionsMu.Unlock()
			started = true
			return id, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancelRun()
	return "", fmt.Errorf("headless daemon %s socket not ready", id)
}

func defaultHeadlessShell() string {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

func (h *harness) readHeadlessSize(ctx context.Context, sessionID string) (int, int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return 0, 0, fmt.Errorf("session_id is required")
	}
	socketPath, err := headless.SocketPath(h.configDir, sessionID)
	if err != nil {
		return 0, 0, err
	}
	dialOptions := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var dialer net.Dialer
					return dialer.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
	ws, _, err := websocket.Dial(ctx, "ws://unix/ws/client", dialOptions)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
		if transport, ok := dialOptions.HTTPClient.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}()
	hello := &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "android-harness-size",
			Cols:         1,
			Rows:         1,
			WantsControl: false,
		}},
	}
	if err := writeProtoFrame(ctx, ws, hello); err != nil {
		return 0, 0, err
	}
	frame, err := readProtoFrame(ctx, ws)
	if err != nil {
		return 0, 0, err
	}
	welcome := frame.GetWelcome()
	if welcome == nil {
		return 0, 0, fmt.Errorf("missing welcome frame")
	}
	return int(welcome.GetServerCols()), int(welcome.GetServerRows()), nil
}

func readProtoFrame(ctx context.Context, conn *websocket.Conn) (*protocolpb.Frame, error) {
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

func writeProtoFrame(ctx context.Context, conn *websocket.Conn, frame *protocolpb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

func startHostSession(ctx context.Context, endpoint, token, id, scriptPath string, cols, rows int, tlsDir string) (context.CancelFunc, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	stdinFile, err := os.Open(os.DevNull)
	if err != nil {
		cancel()
		return nil, err
	}
	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdinFile.Close()
		cancel()
		return nil, err
	}
	var (
		ptyMu    sync.Mutex
		ptyBuf   bytes.Buffer
		ptyDebug = os.Getenv("LINGON_DEBUG_INPUT") == "1"
	)
	runner := session.New(buildHostSessionOptions(endpoint, token, id, scriptPath, cols, rows, tlsDir, stdinFile, stdoutFile, ptyDebug, &ptyMu, &ptyBuf))
	go func() {
		if err := runner.Run(sessionCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("session %s stopped: %v", id, err)
		}
	}()
	return func() {
		cancel()
		_ = stdinFile.Close()
		_ = stdoutFile.Close()
	}, nil
}

func buildHostSessionOptions(endpoint, token, id, scriptPath string, cols, rows int, tlsDir string, stdinFile, stdoutFile *os.File, ptyDebug bool, ptyMu *sync.Mutex, ptyBuf *bytes.Buffer) session.Options {
	return session.Options{
		Endpoint:                    endpoint,
		Token:                       token,
		TLSDir:                      tlsDir,
		SessionID:                   id,
		SessionName:                 id,
		Cols:                        cols,
		Rows:                        rows,
		Shell:                       scriptPath,
		Publish:                     true,
		PublishControl:              true,
		Stdin:                       stdinFile,
		Stdout:                      stdoutFile,
		DisableRaw:                  true,
		DisableDesktopNotifications: true,
		OnPTYRead: func(data []byte) {
			if !ptyDebug || len(data) == 0 {
				return
			}
			ptyMu.Lock()
			defer ptyMu.Unlock()
			_, _ = ptyBuf.Write(data)
			for {
				buf := ptyBuf.Bytes()
				idx := bytes.IndexByte(buf, '\n')
				if idx < 0 {
					break
				}
				line := string(ptyBuf.Next(idx + 1))
				if strings.Contains(line, "ECHO_") || strings.Contains(line, "LINGON_READY") {
					log.Printf("pty.output.%s %s", id, strings.TrimSpace(line))
				}
			}
		},
	}
}

func writeHostScript(baseDir, id, harnessPath string) (string, error) {
	scriptPath := filepath.Join(baseDir, fmt.Sprintf("lingon-host-%s.sh", id))
	if shellPath := strings.TrimSpace(os.Getenv("LINGON_ANDROID_HARNESS_HOST_SHELL")); shellPath != "" {
		content := fmt.Sprintf(`#!/bin/sh
export PS1='%s$ '
exec "%s" -i
`, id, shellPath)
		if err := os.WriteFile(scriptPath, []byte(content), 0o700); err != nil {
			return "", err
		}
		return scriptPath, nil
	}
	content := fmt.Sprintf(`#!/bin/sh
exec "%s" -host-echo -host-id "%s"
`, harnessPath, id)
	if err := os.WriteFile(scriptPath, []byte(content), 0o700); err != nil {
		return "", err
	}
	return scriptPath, nil
}

func runHostEcho(id string) {
	if id == "" {
		id = "host"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var logFile *os.File
	if path := os.Getenv("LINGON_HOST_ECHO_LOG"); path != "" {
		if file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			logFile = file
			defer logFile.Close()
		}
	}
	logf := func(format string, args ...any) {
		if logFile == nil {
			return
		}
		_, _ = fmt.Fprintf(logFile, format+"\n", args...)
	}
	logf("host-echo start id=%s", id)

	if state, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer func() {
			_ = term.Restore(int(os.Stdin.Fd()), state)
		}()
		logf("term.MakeRaw ok")
	} else {
		logf("term.MakeRaw failed err=%v", err)
	}

	writer := bufio.NewWriterSize(os.Stdout, 4096)
	_, _ = fmt.Fprintf(writer, "LINGON_READY %s\r\n", id)
	_ = writer.Flush()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func() {
		i := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(writer, "TICK_%s %d\r\n", id, i)
				_ = writer.Flush()
				i++
			}
		}
	}()

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			logf("read byte=%02x", buf[0])
			_, _ = fmt.Fprintf(writer, "ECHO_%s %02x\r\n", id, buf[0])
			_ = writer.Flush()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return
		}
	}
}

func listenAddr(port int) string {
	if os.Getenv("LINGON_ANDROID_HARNESS_BIND_ALL") != "" {
		if port <= 0 {
			return "0.0.0.0:0"
		}
		return fmt.Sprintf("0.0.0.0:%d", port)
	}
	if port <= 0 {
		return "127.0.0.1:0"
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func listenNetwork() string {
	if os.Getenv("LINGON_ANDROID_HARNESS_BIND_ALL") != "" {
		return "tcp4"
	}
	return "tcp"
}

func ensureBasePath(path string) string {
	if path == "" {
		return "/v1"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func writeConfig(path string, cfg harnessConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func printConfig(cfg harnessConfig) {
	log.Printf("Lingon Android harness ready")
	log.Printf("Endpoint: %s", cfg.Endpoint)
	log.Printf("CA cert: %s", cfg.CACertPath)
	for i, user := range cfg.Users {
		log.Printf("User[%d]: %s", i, user.Username)
		log.Printf("Sessions[%d]: %s", i, strings.Join(user.Sessions, ", "))
	}
	jsonBytes, _ := json.Marshal(cfg)
	fmt.Printf("LINGON_HARNESS_JSON=%s\n", string(jsonBytes))
}
