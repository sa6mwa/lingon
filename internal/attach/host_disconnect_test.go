package attach

import (
	"context"
	"crypto/tls"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestAttachExitsWhenHostDisconnects(t *testing.T) {
	home := t.TempDir()
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

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_disconnect",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/cat"},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		return hub.HasHost("session_disconnect")
	}, hostErr)

	inR, inW := io.Pipe()
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})
	size := &sizeProvider{cols: 80, rows: 24}
	client := &Client{
		Endpoint:       endpoint,
		SessionID:      "session_disconnect",
		AccessToken:    access.Token,
		RequestControl: false,
		ClientID:       "attach1",
		Stdin:          inR,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		TermSize:       size.Size,
	}
	attachCtx, attachCancel := context.WithCancel(context.Background())
	t.Cleanup(attachCancel)
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- client.Run(attachCtx)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		return client.getSnapshot() != nil
	}, hostErr, attachErr)

	hostCancel()

	select {
	case err := <-attachErr:
		if err == nil || !strings.Contains(err.Error(), "host disconnected") {
			t.Fatalf("unexpected attach error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("attach did not exit after host disconnect")
	}
}
