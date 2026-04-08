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

	"golang.org/x/term"

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/tlsmgr"
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
	selfPath   string
	endpoint   string
	access     string
	hostIndex  int
	cols       int
	rows       int
}

type sessionHandle struct {
	id     string
	cancel context.CancelFunc
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

	if *configPath != "" {
		if err := writeConfig(*configPath, h.config); err != nil {
			log.Fatalf("write config: %v", err)
		}
	}
	printConfig(h.config)

	<-ctx.Done()
	h.stop()
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
	hostEchoLog := filepath.Join(os.TempDir(), fmt.Sprintf("lingon-host-echo-%d.log", time.Now().UnixNano()))
	if os.Getenv("LINGON_HOST_ECHO_LOG") == "" {
		_ = os.Setenv("LINGON_HOST_ECHO_LOG", hostEchoLog)
	}
	home, err := os.MkdirTemp("", "lingon-android-harness-")
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("HOME", home); err != nil {
		return nil, err
	}
	tlsDir := filepath.Join(home, ".lingon", "tls")
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
	usersPath := filepath.Join(home, ".lingon", "users.json")
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
	relayServer.UsersFile = usersPath
	relayServer.DataDir = filepath.Join(home, ".lingon")
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
		baseDir:   home,
		selfPath:  selfPath,
		endpoint:  endpoint,
		access:    access.Token,
		hostIndex: 0,
		cols:      opts.cols,
		rows:      opts.rows,
	}

	controlPath := ensureBasePath(opts.basePath) + "/__harness/start-host"
	relayHandler := server.WrapBasePath(opts.basePath, relayServer.Handler())
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
		default:
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
		scriptPath, err := writeHostScript(home, secondID, selfPath)
		if err != nil {
			h.stop()
			return nil, err
		}
		cancel, err := startHostSession(ctx, endpoint, access2.Token, secondID, scriptPath, opts.cols, opts.rows)
		if err != nil {
			h.stop()
			return nil, err
		}
		h.sessions = append(h.sessions, sessionHandle{id: secondID, cancel: cancel})
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

	return h, nil
}

func (h *harness) stop() {
	h.sessionsMu.Lock()
	for _, sess := range h.sessions {
		if sess.cancel != nil {
			sess.cancel()
		}
	}
	h.sessionsMu.Unlock()
	if h.server != nil {
		h.server.CloseClientConnections()
		h.server.Close()
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
	cancel, err := startHostSession(h.ctx, h.endpoint, token, id, scriptPath, h.cols, h.rows)
	if err != nil {
		return "", err
	}
	h.sessionsMu.Lock()
	h.sessions = append(h.sessions, sessionHandle{id: id, cancel: cancel})
	h.sessionsMu.Unlock()
	return id, nil
}

func startHostSession(ctx context.Context, endpoint, token, id, scriptPath string, cols, rows int) (context.CancelFunc, error) {
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
	runner := session.New(session.Options{
		Endpoint:       endpoint,
		Token:          token,
		SessionID:      id,
		SessionName:    id,
		Cols:           cols,
		Rows:           rows,
		Shell:          scriptPath,
		Publish:        true,
		PublishControl: true,
		Stdin:          stdinFile,
		Stdout:         stdoutFile,
		DisableRaw:     true,
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
	})
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

func writeHostScript(baseDir, id, harnessPath string) (string, error) {
	scriptPath := filepath.Join(baseDir, fmt.Sprintf("lingon-host-%s.sh", id))
	content := fmt.Sprintf(`#!/bin/sh
stty raw -echo </dev/tty 2>/dev/null || stty raw -echo </dev/stdin 2>/dev/null || true
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
