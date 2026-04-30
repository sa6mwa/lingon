package session

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestAttachSharedConfigRoot(t *testing.T) {
	runAttachScenario(t, true)
}

func TestAttachSeparateConfigRoot(t *testing.T) {
	runAttachScenario(t, false)
}

func runAttachScenario(t *testing.T, sharedConfig bool) {
	waitFast := 2 * time.Second
	hostConfigDir := testutil.SetLingonConfigEnv(t)
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
	_, httpURL, err := normalizeEndpoint(endpoint)
	if err != nil {
		t.Fatalf("normalizeEndpoint: %v", err)
	}

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

	attachAuthPath := hostAuthPath
	if !sharedConfig {
		attachConfigDir := testutil.SetLingonConfigEnv(t)
		attachAuthPath = filepath.Join(attachConfigDir, "auth.json")
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

	uiMaster, uiSlave, err := pty.Open()
	if err != nil {
		t.Fatalf("host pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = uiMaster.Close()
		_ = uiSlave.Close()
	})
	_ = pty.Setsize(uiSlave, &pty.Winsize{Cols: 80, Rows: 24})
	go func() {
		_, _ = io.Copy(io.Discard, uiMaster)
	}()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("host stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutR)
	}()

	hostOut := &lockedString{}
	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)

	runner := New(Options{
		Endpoint:       endpoint,
		TLSDir:         tlsDir,
		Token:          access.Token,
		AuthFile:       hostAuthPath,
		SessionID:      "session_attach",
		Cols:           80,
		Rows:           24,
		Shell:          "/bin/sh",
		Publish:        true,
		PublishControl: true,
		Stdin:          uiSlave,
		Stdout:         stdoutW,
		DisableRaw:     true,
		OnPTYRead: func(data []byte) {
			_, _ = hostOut.Write(data)
		},
	})

	hostErr := make(chan error, 1)
	go func() {
		hostErr <- runner.Run(hostCtx)
	}()
	waitUntilAll(t, waitFast, func() bool {
		return hub.HasHost("session_attach")
	}, hostErr)

	tlsPool, err := tlsmgr.LoadLocalCARoots(tlsDir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: tlsPool, MinVersion: tls.VersionTLS12}},
	}
	req, err := http.NewRequestWithContext(context.Background(), "GET", endpoint+"/sessions", nil)
	if err != nil {
		t.Fatalf("sessions request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("sessions request failed: %v", err)
	}
	if resp.Body == nil {
		t.Fatalf("sessions response empty")
	}
	var listed []relay.Session
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode sessions: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("sessions status: %d", resp.StatusCode)
	}
	if len(listed) == 0 {
		t.Fatalf("sessions list empty")
	}
	found := false
	for _, s := range listed {
		if s.ID == "session_attach" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session_attach not listed: %+v", listed)
	}

	attachMaster, attachSlave, err := pty.Open()
	if err != nil {
		t.Fatalf("attach pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = attachMaster.Close()
		_ = attachSlave.Close()
	})
	_ = pty.Setsize(attachSlave, &pty.Winsize{Cols: 80, Rows: 24})

	attachOut := &lockedString{}
	go func() {
		_, _ = io.Copy(attachOut, attachMaster)
	}()

	attachState, err := relayclient.EnsureAccessTokenWithTLSDir(context.Background(), endpoint, attachAuthPath, tlsDir)
	if err != nil {
		t.Fatalf("ensureAccessToken: %v", err)
	}
	attachClient := &attach.MultiClient{
		Endpoint:       endpoint,
		TLSDir:         tlsDir,
		AccessToken:    attachState.AccessToken,
		RequestControl: true,
		SessionID:      "session_attach",
		Stdin:          attachSlave,
		Stdout:         attachSlave,
		Stderr:         attachSlave,
		TermSize: func() (int, int) {
			return 80, 24
		},
		DisableSignalResize: true,
	}

	attachCtx, attachCancel := context.WithCancel(context.Background())
	t.Cleanup(attachCancel)
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.Run(attachCtx)
	}()

	waitUntilAll(t, waitFast, func() bool {
		return hub.ClientCount("session_attach") > 0
	}, hostErr, attachErr)

	waitUntilAll(t, waitFast, func() bool {
		return attachOut.String() != ""
	}, hostErr, attachErr)

	_, _ = uiMaster.Write([]byte("echo HOST_READY\n"))
	waitUntilAll(t, waitFast, func() bool {
		return strings.Contains(hostOut.String(), "HOST_READY")
	}, hostErr, attachErr)

	waitUntilAll(t, waitFast, func() bool {
		return strings.Contains(attachOut.String(), "HOST_READY")
	}, hostErr, attachErr)

	_, _ = attachMaster.Write([]byte("echo ATTACH_READY\n"))
	waitUntilAll(t, waitFast, func() bool {
		return strings.Contains(hostOut.String(), "ATTACH_READY") && strings.Contains(attachOut.String(), "ATTACH_READY")
	}, hostErr, attachErr)

	_, _ = attachMaster.Write([]byte("echo ATTACH_SECOND\n"))
	waitUntilAll(t, waitFast, func() bool {
		return strings.Contains(hostOut.String(), "ATTACH_SECOND") && strings.Contains(attachOut.String(), "ATTACH_SECOND")
	}, hostErr, attachErr)

	_, _ = attachMaster.Write([]byte{0x04})
	select {
	case err := <-attachErr:
		if err != nil {
			t.Fatalf("attach error: %v", err)
		}
	case <-time.After(waitFast):
		t.Fatalf("attach did not exit on ctrl+d")
	}

	_, _ = uiMaster.Write([]byte("echo HOST_ALIVE\n"))
	waitUntilAll(t, waitFast, func() bool {
		return strings.Contains(hostOut.String(), "HOST_ALIVE")
	}, hostErr)

	hostCancel()
	select {
	case <-hostErr:
	case <-time.After(waitFast):
		t.Fatalf("host did not exit")
	}
}
