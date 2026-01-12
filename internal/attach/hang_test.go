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

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestAttachFailsWithoutHost(t *testing.T) {
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

	stdinR, stdinW := io.Pipe()
	defer func() {
		_ = stdinW.Close()
	}()

	client := &Client{
		Endpoint:       endpoint,
		SessionID:      "session_test",
		AccessToken:    access.Token,
		RequestControl: true,
		ClientID:       "client1",
		Stdin:          stdinR,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		TermSize: func() (int, int) {
			return 80, 24
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- client.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected error when no host is connected")
		}
		if !strings.Contains(err.Error(), "no host connected") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("attach hung without host")
	}
}
