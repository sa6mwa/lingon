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
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/tlsmgr"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type sizeProvider struct {
	mu   sync.RWMutex
	cols int
	rows int
}

func (s *sizeProvider) Size() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cols, s.rows
}

func (s *sizeProvider) Set(cols, rows int) {
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
}

func TestResizeKeepsSessionResponsive(t *testing.T) {
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

	stdoutMaster, stdoutSlave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutMaster.Close()
		_ = stdoutSlave.Close()
	})
	_ = pty.Setsize(stdoutMaster, &pty.Winsize{Cols: 80, Rows: 24})
	go func() {
		_, _ = io.Copy(io.Discard, stdoutMaster)
	}()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	scriptPath := filepath.Join(t.TempDir(), "emit.sh")
	script := "#!/bin/sh\nprintf \"one\\n\"\nsleep 0.3\nprintf \"two\\n\"\nsleep 5\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ptyOut := &lockedBuffer{}
	frameOut := &lockedBuffer{}
	snapOut := &lockedBuffer{}
	runner := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_test",
		Cols:       80,
		Rows:       24,
		Shell:      scriptPath,
		Term:       "tmux-256color",
		Publish:    true,
		Stdin:      inR,
		Stdout:     stdoutSlave,
		DisableRaw: true,
		OnPTYRead: func(data []byte) {
			_, _ = ptyOut.Write(data)
		},
		OnPublishFrame: func(frame *protocolpb.Frame) {
			if frameContains(frame, "two") {
				_, _ = frameOut.Write([]byte("two"))
			}
		},
		OnSnapshot: func(s terminal.Snapshot) {
			if strings.Contains(snapshotRunes(s), "two") {
				_, _ = snapOut.Write([]byte("two"))
			}
		},
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		return hub.HasHost("session_test")
	}, runErr)

	out := &lockedBuffer{}
	size := &sizeProvider{cols: 80, rows: 24}
	attachClient := &attach.Client{
		Endpoint:       endpoint,
		SessionID:      "session_test",
		AccessToken:    access.Token,
		RequestControl: false,
		ClientID:       "attach1",
		Stdin:          strings.NewReader(""),
		Stdout:         out,
		Stderr:         io.Discard,
		TermSize:       size.Size,
	}
	attachCtx, attachCancel := context.WithCancel(context.Background())
	t.Cleanup(attachCancel)
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.Run(attachCtx)
	}()

	time.Sleep(150 * time.Millisecond)
	_ = pty.Setsize(stdoutMaster, &pty.Winsize{Cols: 100, Rows: 30})
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)

	if !waitForOutput(t, out, "two") {
		t.Fatalf("attach missing 'two'; attach=%q pty=%q frames=%q snaps=%q", out.String(), ptyOut.String(), frameOut.String(), snapOut.String())
	}
	if !strings.Contains(ptyOut.String(), "two") {
		t.Fatalf("pty output missing 'two': %q", ptyOut.String())
	}
	if !strings.Contains(frameOut.String(), "two") {
		t.Fatalf("publish frames missing 'two'")
	}

	attachCancel()
	cancel()
	_ = stdoutMaster.Close()

	select {
	case <-attachErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("attach did not exit")
	}
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		default:
		}
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func waitForOutput(t *testing.T, out *lockedBuffer, want string) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func frameContains(frame *protocolpb.Frame, want string) bool {
	if frame == nil {
		return false
	}
	if snap := frame.GetSnapshot(); snap != nil {
		return strings.Contains(snapshotString(snap), want)
	}
	if diff := frame.GetDiff(); diff != nil {
		return strings.Contains(diffString(diff), want)
	}
	return false
}

func snapshotString(snap *protocolpb.Snapshot) string {
	if snap == nil {
		return ""
	}
	var b strings.Builder
	for _, r := range snap.Runes {
		b.WriteRune(rune(r))
	}
	return b.String()
}

func diffString(diff *protocolpb.Diff) string {
	if diff == nil {
		return ""
	}
	var b strings.Builder
	for _, row := range diff.DiffRows {
		for _, r := range row.Runes {
			b.WriteRune(rune(r))
		}
	}
	return b.String()
}

func snapshotRunes(snap terminal.Snapshot) string {
	var b strings.Builder
	for _, cell := range snap.Cells {
		b.WriteRune(cell.Rune)
	}
	return b.String()
}
