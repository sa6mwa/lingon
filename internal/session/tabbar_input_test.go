package session

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestHostTabBarToggleDoesNotStallInput(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(home, ".lingon", "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	shellPath := "/bin/sh"

	uiMasterA, uiSlaveA, err := pty.Open()
	if err != nil {
		t.Fatalf("pty A open: %v", err)
	}
	t.Cleanup(func() {
		_ = uiMasterA.Close()
		_ = uiSlaveA.Close()
	})
	_ = pty.Setsize(uiSlaveA, &pty.Winsize{Cols: 80, Rows: 24})
	go func() {
		_, _ = io.Copy(io.Discard, uiMasterA)
	}()

	stdoutAR, stdoutAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout A pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutAR.Close()
		_ = stdoutAW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutAR)
	}()

	stdoutBR, stdoutBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutBR.Close()
		_ = stdoutBW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutBR)
	}()

	inBR, inBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inBR.Close()
		_ = inBW.Close()
	})

	outA := &lockedString{}
	outB := &lockedString{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runnerA := New(Options{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_a",
		Cols:      80,
		Rows:      24,
		Shell:     shellPath,
		Publish:   true,
		Stdin:     uiSlaveA,
		Stdout:    stdoutAW,
		OnPTYRead: func(data []byte) {
			_, _ = outA.Write(data)
		},
	})
	runnerB := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_b",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inBR,
		Stdout:     stdoutBW,
		DisableRaw: true,
		OnPTYRead: func(data []byte) {
			_, _ = outB.Write(data)
		},
	})

	runErrA := make(chan error, 1)
	runErrB := make(chan error, 1)
	go func() {
		runErrB <- runnerB.Run(ctx)
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_b")
	}, runErrB)

	go func() {
		runErrA <- runnerA.Run(ctx)
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_a")
	}, runErrA, runErrB)

	waitUntilAll(t, 5*time.Second, func() bool {
		return runnerA.remoteSessions != nil
	}, runErrA, runErrB)
	var sessions []remoteSessionInfo
	waitUntilAll(t, 15*time.Second, func() bool {
		refreshCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		err := runnerA.remoteSessions.refreshSessions(refreshCtx)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh sessions: %v", err)
		}
		sessions = runnerA.remoteSessions.Sessions()
		return len(sessions) >= 2
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte{0x0c, 'n'})
	waitUntilAll(t, 5*time.Second, func() bool {
		activeID, activeLocal := runnerA.activeSession()
		return activeID == "session_b" && !activeLocal
	}, runErrA, runErrB)
	waitUntilAll(t, 5*time.Second, func() bool {
		view := runnerA.remoteSessions.views["session_b"]
		if view == nil || view.client == nil {
			return false
		}
		if view.client.ClientID == "" {
			return false
		}
		return hub.HasClientID("session_b", view.client.ClientID)
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte("echo READY_B\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		return strings.Contains(outB.String(), "READY_B")
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte{0x0c, 'p'})
	waitUntilAll(t, 5*time.Second, func() bool {
		activeID, activeLocal := runnerA.activeSession()
		return activeLocal && activeID == "session_a"
	}, runErrA, runErrB)
	_, _ = uiMasterA.Write([]byte("echo LOCAL_OK\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		return strings.Contains(outA.String(), "LOCAL_OK")
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte{0x0c, 'n'})
	waitUntilAll(t, 5*time.Second, func() bool {
		activeID, activeLocal := runnerA.activeSession()
		return activeID == "session_b" && !activeLocal
	}, runErrA, runErrB)
	waitUntilAll(t, 5*time.Second, func() bool {
		view := runnerA.remoteSessions.views["session_b"]
		return view != nil && !view.needsFullRender
	}, runErrA, runErrB)
	_, _ = uiMasterA.Write([]byte("echo REMOTE_OK\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		return strings.Contains(outB.String(), "REMOTE_OK")
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte{0x0c, 'q'})

	_ = inBW.Close()
	cancel()

	select {
	case <-runErrA:
	case <-time.After(5 * time.Second):
		t.Fatalf("session A did not exit")
	}
	select {
	case <-runErrB:
	case <-time.After(5 * time.Second):
		t.Fatalf("session B did not exit")
	}
}

func TestSessionStreamUpdatesTabs(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(home, ".lingon", "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	shellPath := "/bin/sh"

	inAR, inAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin A pipe: %v", err)
	}
	outAR, outAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout A pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inAR.Close()
		_ = inAW.Close()
		_ = outAR.Close()
		_ = outAW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, outAR)
	}()

	inBR, inBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin B pipe: %v", err)
	}
	outBR, outBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inBR.Close()
		_ = inBW.Close()
		_ = outBR.Close()
		_ = outBW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, outBR)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runnerA := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_a",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inAR,
		Stdout:     outAW,
		DisableRaw: true,
	})
	runnerB := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_b",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inBR,
		Stdout:     outBW,
		DisableRaw: true,
	})

	runErrA := make(chan error, 1)
	runErrB := make(chan error, 1)
	go func() {
		runErrA <- runnerA.Run(ctx)
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_a")
	}, runErrA)

	waitUntilAll(t, 5*time.Second, func() bool {
		if runnerA.remoteSessions == nil {
			return false
		}
		return len(runnerA.remoteSessions.Sessions()) >= 1
	}, runErrA)

	go func() {
		runErrB <- runnerB.Run(ctx)
	}()
	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_b")
	}, runErrA, runErrB)

	waitUntilAll(t, 5*time.Second, func() bool {
		return runnerA.remoteSessions != nil && len(runnerA.remoteSessions.Sessions()) >= 2
	}, runErrA, runErrB)

	_ = inAW.Close()
	_ = inBW.Close()
	cancel()

	select {
	case <-runErrA:
	case <-time.After(5 * time.Second):
		t.Fatalf("session A did not exit")
	}
	select {
	case <-runErrB:
	case <-time.After(5 * time.Second):
		t.Fatalf("session B did not exit")
	}
}

func TestRemoteInputSurvivesStaleSessionsList(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(home, ".lingon", "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	shellPath := "/bin/sh"

	uiMasterA, uiSlaveA, err := pty.Open()
	if err != nil {
		t.Fatalf("pty A open: %v", err)
	}
	t.Cleanup(func() {
		_ = uiMasterA.Close()
		_ = uiSlaveA.Close()
	})
	_ = pty.Setsize(uiSlaveA, &pty.Winsize{Cols: 80, Rows: 24})
	go func() {
		_, _ = io.Copy(io.Discard, uiMasterA)
	}()

	stdoutAR, stdoutAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout A pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutAR.Close()
		_ = stdoutAW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutAR)
	}()

	stdoutBR, stdoutBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutBR.Close()
		_ = stdoutBW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutBR)
	}()

	inBR, inBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inBR.Close()
		_ = inBW.Close()
	})

	outB := &lockedString{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runnerA := New(Options{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_a",
		Cols:      80,
		Rows:      24,
		Shell:     shellPath,
		Publish:   true,
		Stdin:     uiSlaveA,
		Stdout:    stdoutAW,
	})
	runnerB := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_b",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inBR,
		Stdout:     stdoutBW,
		DisableRaw: true,
		OnPTYRead: func(data []byte) {
			_, _ = outB.Write(data)
		},
	})

	runErrA := make(chan error, 1)
	runErrB := make(chan error, 1)
	go func() {
		runErrB <- runnerB.Run(ctx)
	}()
	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_b")
	}, runErrB)

	go func() {
		runErrA <- runnerA.Run(ctx)
	}()
	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_a")
	}, runErrA, runErrB)

	if runnerA.remoteSessions == nil {
		t.Fatalf("remote sessions not initialized")
	}
	waitUntilAll(t, 10*time.Second, func() bool {
		if err := runnerA.remoteSessions.refreshSessions(ctx); err != nil {
			t.Fatalf("refresh sessions: %v", err)
		}
		return len(runnerA.remoteSessions.Sessions()) >= 2
	}, runErrA, runErrB)

	_, _ = uiMasterA.Write([]byte{0x0c, 'n'})
	waitUntilAll(t, 5*time.Second, func() bool {
		activeID, activeLocal := runnerA.activeSession()
		return activeID == "session_b" && !activeLocal
	}, runErrA, runErrB)

	runnerA.remoteSessions.mu.Lock()
	runnerA.remoteSessions.sessions = nil
	runnerA.remoteSessions.mu.Unlock()

	_, _ = uiMasterA.Write([]byte("echo STALE\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		return strings.Contains(outB.String(), "STALE")
	}, runErrA, runErrB)

	_ = inBW.Close()
	cancel()

	select {
	case <-runErrA:
	case <-time.After(5 * time.Second):
		var buf bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
		t.Fatalf("session A did not exit\n%s", buf.String())
	}
	select {
	case <-runErrB:
	case <-time.After(5 * time.Second):
		t.Fatalf("session B did not exit")
	}
}
