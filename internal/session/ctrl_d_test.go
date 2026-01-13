package session

import (
	"context"
	"crypto/tls"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/tlsmgr"
)

type lockedString struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedString) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedString) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAttachCtrlDDoesNotExitHost(t *testing.T) {
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

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutR)
	}()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	ptyOut := &lockedString{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	scriptPath := filepath.Join(t.TempDir(), "reader.sh")
	script := "#!/bin/sh\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    *ECHO*) echo STILL ;;\n" +
		"  esac\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_ctrl_d",
		Cols:       80,
		Rows:       24,
		Shell:      scriptPath,
		Publish:    true,
		Stdin:      inR,
		Stdout:     stdoutW,
		DisableRaw: true,
		OnPTYRead: func(data []byte) {
			_, _ = ptyOut.Write(data)
		},
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		return hub.HasHost("session_ctrl_d")
	}, runErr)

	attachIn, attachW := io.Pipe()
	t.Cleanup(func() {
		_ = attachIn.Close()
		_ = attachW.Close()
	})

	size := &sizeProvider{cols: 80, rows: 24}
	attachClient := &attach.Client{
		Endpoint:       endpoint,
		SessionID:      "session_ctrl_d",
		AccessToken:    access.Token,
		RequestControl: true,
		ClientID:       "attach1",
		Stdin:          attachIn,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		TermSize:       size.Size,
	}
	attachCtx, attachCancel := context.WithCancel(context.Background())
	t.Cleanup(attachCancel)
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.Run(attachCtx)
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.ControllerID("session_ctrl_d") == "attach1"
	}, runErr, attachErr)

	if runner.ttyFile != nil {
		waitUntilAll(t, 5*time.Second, func() bool {
			veof, err := getVEOF(runner.ttyFile)
			if err != nil {
				return false
			}
			return veof == 0
		}, runErr, attachErr)
	}

	_, _ = attachW.Write([]byte{0x04})

	select {
	case err := <-attachErr:
		if err != nil {
			t.Fatalf("attach error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("attach did not exit")
	}

	_, _ = inW.Write([]byte("ECHO\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		return strings.Contains(ptyOut.String(), "STILL")
	}, runErr)

	_ = inW.Close()
	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func waitUntilAll(t *testing.T, timeout time.Duration, cond func() bool, errChs ...<-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
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
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}
