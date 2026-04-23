package session

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestNo401OnExpiredAuthAfterRelayRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	root := testutil.SetXDGConfigEnv(t)
	configDir := filepath.Join(root, config.DefaultConfigDirName)
	tlsDir := filepath.Join(configDir, "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(configDir, "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	now := time.Now().UTC()
	store := relay.NewStore()
	refresh, err := store.CreateRefreshToken("test", time.Hour, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	access, err := store.CreateAccessTokenForRefresh("test", refresh.Token, "host", -1*time.Minute, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	dataDir := configDir
	if err := store.Save(dataDir); err != nil {
		t.Fatalf("Save store: %v", err)
	}

	authPath := filepath.Join(configDir, "auth.json")
	state := authstore.State{
		Endpoint:         "https://127.0.0.1:0/v1",
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatalf("Save auth: %v", err)
	}

	var unauthorized int64
	makeHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{
				ResponseWriter: w,
				onWrite: func(code int) {
					if code == http.StatusUnauthorized {
						atomic.AddInt64(&unauthorized, 1)
					}
				},
			}
			h.ServeHTTP(rec, r)
		})
	}

	endpoint, hub, stopServer, listenAddr := startRelayServer(t, dataDir, usersPath, cert, "", makeHandler)
	defer stopServer()

	if state.Endpoint == "https://127.0.0.1:0/v1" {
		state.Endpoint = endpoint
		if err := authstore.Save(authPath, state); err != nil {
			t.Fatalf("Save auth endpoint: %v", err)
		}
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sessionID := "session_auth_preflight"
	runner := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		AuthFile:   authPath,
		SessionID:  sessionID,
		Cols:       80,
		Rows:       24,
		Shell:      "/bin/sh",
		Publish:    true,
		Stdin:      inR,
		Stdout:     outW,
		DisableRaw: true,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	waitUntilNoErr(t, runner.clock, 2*time.Second, func() bool {
		return hub.HasHost(sessionID)
	})

	attachInR, attachInW, err := os.Pipe()
	if err != nil {
		t.Fatalf("attach stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = attachInR.Close()
		_ = attachInW.Close()
	})
	attachOutR, attachOutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("attach stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = attachOutR.Close()
		_ = attachOutW.Close()
	})

	size := func() (int, int) { return 80, 24 }
	multi := &attach.MultiClient{
		Endpoint:            endpoint,
		SessionID:           sessionID,
		AccessToken:         access.Token,
		AuthFile:            authPath,
		RequestControl:      true,
		Stdin:               attachInR,
		Stdout:              attachOutW,
		Stderr:              io.Discard,
		TermSize:            size,
		DisableSignalResize: true,
		RefreshInterval:     200 * time.Millisecond,
		BackoffPolicy: backoff.Policy{
			Base:   50 * time.Millisecond,
			Factor: 1.5,
			Max:    200 * time.Millisecond,
		},
	}
	attachDone := make(chan struct{})
	var attachRunErr error
	go func() {
		attachRunErr = multi.Run(ctx)
		close(attachDone)
	}()

	waitUntilNoErr(t, runner.clock, 4*time.Second, func() bool {
		return hub.ClientCount(sessionID) >= 1
	})

	if count := atomic.LoadInt64(&unauthorized); count != 0 {
		t.Fatalf("unexpected 401 responses before restart: %d", count)
	}

	stopServer()
	_, hub, stopServer, _ = startRelayServer(t, dataDir, usersPath, cert, listenAddr, makeHandler)
	defer stopServer()

	waitUntilNoErr(t, runner.clock, 4*time.Second, func() bool {
		return hub.HasHost(sessionID)
	})
	waitUntilNoErr(t, runner.clock, 30*time.Second, func() bool {
		if hub.ClientCount(sessionID) >= 1 {
			return true
		}
		select {
		case <-attachDone:
			if attachRunErr != nil && !errors.Is(attachRunErr, context.Canceled) && !strings.Contains(attachRunErr.Error(), "no sessions available") {
				t.Fatalf("attach exited unexpectedly after restart: %v", attachRunErr)
			}
			return true
		default:
			return false
		}
	})

	if count := atomic.LoadInt64(&unauthorized); count != 0 {
		t.Fatalf("unexpected 401 responses after restart: %d", count)
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatalf("host runner did not exit after cancel")
	}
	select {
	case <-attachDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("attach client did not exit after cancel")
	}
}

type statusRecorder struct {
	http.ResponseWriter
	onWrite func(code int)
	status  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
		if r.onWrite != nil {
			r.onWrite(code)
		}
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacker not supported")
	}
	return h.Hijack()
}

func startRelayServer(t *testing.T, dataDir, usersPath string, cert tls.Certificate, addr string, wrap func(http.Handler) http.Handler) (string, *relay.Hub, func(), string) {
	t.Helper()
	store, err := relay.LoadStore(dataDir)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	users, err := relay.LoadUserStore(usersPath)
	if err != nil {
		t.Fatalf("LoadUserStore: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = dataDir

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	if wrap != nil {
		handler = wrap(handler)
	}

	listenAddr := addr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = listener.Addr().String()
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv := &http.Server{Handler: handler, TLSConfig: tlsCfg}
	tlsListener := tls.NewListener(listener, tlsCfg)
	go func() {
		_ = srv.Serve(tlsListener)
	}()

	stop := func() {
		relayServer.Close("shutdown")
		_ = srv.Close()
	}
	return "https://" + addr + "/v1", hub, stop, addr
}
