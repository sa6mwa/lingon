package session

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/authstore"
)

func TestHostLocalPTYAcceptsInputWhileAuthRefreshIsBlocked(t *testing.T) {
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var refreshOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/refresh" {
			http.NotFound(w, r)
			return
		}
		refreshOnce.Do(func() { close(refreshStarted) })
		select {
		case <-releaseRefresh:
		case <-r.Context().Done():
		}
		http.Error(w, "blocked test refresh", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(releaseRefresh)

	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := authstore.Save(authPath, authstore.State{
		Endpoint:         server.URL,
		AccessToken:      "expired-access",
		AccessExpiresAt:  time.Now().Add(-time.Hour),
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save auth state: %v", err)
	}
	shellPath := filepath.Join(t.TempDir(), "preconnect-shell.sh")
	const shellScript = `#!/usr/bin/env bash
set -u
stty -echo
trap 'stty sane 2>/dev/null || true' EXIT INT TERM
printf 'READY>'
while IFS= read -r line; do
  printf '\r\nPRECONNECT_OK\r\n'
done
`
	if err := os.WriteFile(shellPath, []byte(shellScript), 0o755); err != nil {
		t.Fatalf("write preconnect shell: %v", err)
	}

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer func() { _ = master.Close() }()
	defer func() { _ = slave.Close() }()
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}

	reads := newAsyncPTYReader(t, master)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := New(Options{
		Endpoint:  server.URL,
		AuthFile:  authPath,
		SessionID: "preconnect-input",
		Cols:      80,
		Rows:      24,
		Shell:     shellPath,
		Publish:   true,
		Stdin:     slave,
		Stdout:    slave,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher did not begin auth refresh")
	}
	if !reads.waitContains("READY>", 2*time.Second) {
		t.Fatalf("local PTY did not start while auth refresh was blocked; output:\n%s", reads.string())
	}

	if _, err := master.Write([]byte("x\r")); err != nil {
		t.Fatalf("write pre-connect input: %v", err)
	}
	if !reads.waitContains("PRECONNECT_OK", 2*time.Second) {
		t.Fatalf("local PTY did not accept input while auth refresh was blocked; output:\n%s", reads.string())
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && !strings.Contains(err.Error(), "file already closed") && err != context.Canceled {
			t.Fatalf("runner returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}

type asyncPTYReader struct {
	t  *testing.T
	mu sync.Mutex
	b  bytes.Buffer
}

func newAsyncPTYReader(t *testing.T, file *os.File) *asyncPTYReader {
	t.Helper()
	r := &asyncPTYReader{t: t}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := file.Read(buf)
			if n > 0 {
				r.mu.Lock()
				_, _ = r.b.Write(buf[:n])
				r.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *asyncPTYReader) waitContains(marker string, timeout time.Duration) bool {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(r.string(), marker) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return strings.Contains(r.string(), marker)
}

func (r *asyncPTYReader) string() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}
