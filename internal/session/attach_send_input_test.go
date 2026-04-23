package session_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestAttachSendInputSharedConfig(t *testing.T) {
	runSendInputScenario(t, true)
}

func TestAttachSendInputSeparateConfig(t *testing.T) {
	runSendInputScenario(t, false)
}

type byteCollector struct {
	mu  sync.Mutex
	buf []byte
}

func (c *byteCollector) Add(data []byte) {
	if len(data) == 0 {
		return
	}
	c.mu.Lock()
	c.buf = append(c.buf, data...)
	c.mu.Unlock()
}

func (c *byteCollector) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]byte, len(c.buf))
	copy(out, c.buf)
	return out
}

func waitUntilAll(t *testing.T, timeout time.Duration, cond func() bool, errChs ...<-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		for _, errCh := range errChs {
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				t.Fatalf("unexpected early exit")
			default:
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func runSendInputScenario(t *testing.T, sharedConfig bool) {
	hostRoot := testutil.SetXDGConfigEnv(t)
	hostConfigDir := filepath.Join(hostRoot, lingon.DefaultConfigDirName)
	tlsDir := filepath.Join(hostConfigDir, "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(hostConfigDir, "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	now := time.Now().UTC()
	refresh, err := store.CreateRefreshTokenForClient("test", "cli", time.Hour, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	access, err := store.CreateAccessTokenForRefresh("test", refresh.Token, "cli", time.Minute, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = hostConfigDir

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	httpURL := endpoint

	hostAuthPath := filepath.Join(hostConfigDir, "auth.json")
	authState := authstore.State{
		Endpoint:         httpURL,
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	}
	if err := authstore.Save(hostAuthPath, authState); err != nil {
		t.Fatalf("save host auth: %v", err)
	}

	if !sharedConfig {
		attachRoot := testutil.SetXDGConfigEnv(t)
		attachConfigDir := filepath.Join(attachRoot, lingon.DefaultConfigDirName)
		attachAuthPath := filepath.Join(attachConfigDir, "auth.json")
		if err := authstore.Save(attachAuthPath, authState); err != nil {
			t.Fatalf("save attach auth: %v", err)
		}
		caSrc := filepath.Join(hostConfigDir, "tls", "ca.pem")
		caDstDir := filepath.Join(attachConfigDir, "tls")
		if err := os.MkdirAll(caDstDir, 0o700); err != nil {
			t.Fatalf("mkdir attach tls: %v", err)
		}
		data, err := os.ReadFile(caSrc)
		if err != nil {
			t.Fatalf("read ca: %v", err)
		}
		if err := os.WriteFile(filepath.Join(caDstDir, "ca.pem"), data, 0o600); err != nil {
			t.Fatalf("write attach ca: %v", err)
		}
	}

	hostInput := &byteCollector{}

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)

	hostErr := make(chan error, 1)
	hostSession := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_send",
		Cols:      80,
		Rows:      24,
		// Run in raw mode so control-byte tokens (e.g. C-c, C-d) are treated as input
		// bytes instead of terminating the foreground process and flaking this test.
		Command: []string{"/bin/sh", "-c", "stty raw -echo; cat >/dev/null"},
		OnInput: func(data []byte) {
			hostInput.Add(data)
		},
	}
	go func() {
		hostErr <- hostSession.Run(hostCtx)
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_send")
	}, hostErr)

	sendOpts := lingon.SendInputOptions{
		Endpoint:       endpoint,
		SessionID:      "session_send",
		AccessToken:    access.Token,
		RequestControl: true,
	}

	sendOpts.NoNewline = true
	sendOpts.Tokens = []string{"START{ENTER}A{ESC}{UP}{DOWN}{LEFT}{RIGHT}{TAB}{BS}{DEL}{C-a}{C-e}{C-c}{C-d}{C-j}{ENTER}END{ENTER}"}
	if err := lingon.SendInput(context.Background(), sendOpts); err != nil {
		t.Fatalf("SendInput tokens: %v", err)
	}
	sendOpts.NoNewline = false

	waitUntilAll(t, 5*time.Second, func() bool {
		out := string(hostInput.Bytes())
		return strings.Contains(out, "START\n") && strings.Contains(out, "END\n")
	}, hostErr)

	expected := []byte{65, 27, 27, 91, 65, 27, 91, 66, 27, 91, 68, 27, 91, 67, 9, 8, 127, 1, 5, 3, 4, 10, 10}
	got := extractBytesBetweenMarkers(hostInput.Bytes(), []byte("START\n"), []byte("END\n"))
	if len(got) < len(expected) {
		t.Fatalf("expected at least %d bytes, got %d (%v)", len(expected), len(got), got)
	}
	got = got[:len(expected)]
	for i, want := range expected {
		if got[i] != want {
			t.Fatalf("byte %d mismatch: got %d want %d (full=%v)", i, got[i], want, got)
		}
	}

	hostCancel()
	select {
	case <-hostErr:
	case <-time.After(5 * time.Second):
		t.Fatalf("host did not exit")
	}
}

func extractBytesBetweenMarkers(out, startMarker, endMarker []byte) []byte {
	startIdx := bytes.Index(out, startMarker)
	if startIdx == -1 {
		return nil
	}
	startIdx += len(startMarker)
	endIdx := bytes.Index(out[startIdx:], endMarker)
	if endIdx == -1 {
		return nil
	}
	return out[startIdx : startIdx+endIdx]
}
