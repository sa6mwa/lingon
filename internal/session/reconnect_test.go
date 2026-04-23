package session

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestHostReconnectsAfterServerRestart(t *testing.T) {
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

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = configDir

	handler := server.WrapBasePath("/v1", relayServer.Handler())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	server1 := &http.Server{Handler: handler, TLSConfig: tlsCfg}
	tlsListener := tls.NewListener(listener, tlsCfg)
	go func() {
		_ = server1.Serve(tlsListener)
	}()

	endpoint := "https://" + addr + "/v1"

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

	runner := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_reconnect",
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

	waitUntil(t, 5*time.Second, func() bool {
		return hub.HasHost("session_reconnect")
	}, runErr)

	relayServer.Close("shutdown")
	_ = server1.Close()

	select {
	case err := <-runErr:
		t.Fatalf("host runner exited after server stop: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	var listener2 net.Listener
	for i := 0; i < 5; i++ {
		listener2, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if listener2 == nil {
		t.Fatalf("listen restart: %v", err)
	}
	hub2 := relay.NewHub(nil)
	relayServer2 := relay.NewHTTPServer(store, users, auth, nil, hub2)
	relayServer2.UsersFile = usersPath
	relayServer2.DataDir = configDir
	handler2 := server.WrapBasePath("/v1", relayServer2.Handler())
	server2 := &http.Server{Handler: handler2, TLSConfig: tlsCfg}
	tlsListener2 := tls.NewListener(listener2, tlsCfg)
	defer func() {
		_ = server2.Close()
	}()
	go func() {
		_ = server2.Serve(tlsListener2)
	}()

	waitUntil(t, 20*time.Second, func() bool {
		return hub2.HasHost("session_reconnect")
	}, runErr)

	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatalf("host runner did not exit after cancel")
	}
}
