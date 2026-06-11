package attach_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/pslog"
)

func TestMultiAttachLocalHeadlessShowsLoadingWhileWaitingForSnapshot(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "headless.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		accepted <- conn
	}()
	t.Cleanup(func() {
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	h := newHarness(t)
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless",
		Cols:     80,
		Rows:     24,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "waiting-local"}}, nil
		},
		SocketResolver: func(sessionID string) (string, error) {
			if sessionID != "waiting-local" {
				return "", fmt.Errorf("unexpected session %q", sessionID)
			}
			return socketPath, nil
		},
	})
	t.Cleanup(sess.Cancel)

	h.Advance(time.Millisecond)
	var raw string
	sess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		raw += sess.DrainRaw()
		if strings.Contains(screen.String(), "loading local headless") {
			return nil
		}
		return fmt.Errorf("expected local loading status instead of blank screen:\n%s", screen.String())
	})
	if !ptytest.HasFullRedrawANSI(raw, 24) {
		t.Fatalf("expected local loading render to clear stale terminal contents, raw=%q", raw)
	}
}

func TestMultiAttachLocalHeadlessClearsLoadingAfterFirstSnapshot(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("sh not available")
	}

	cfgDir := t.TempDir()
	const sessionID = "ready-local"
	shellPath := filepath.Join(t.TempDir(), "ready-shell.sh")
	script := "#!/bin/sh\nwhile :; do printf 'LOCAL_READY\\n'; sleep 1; done\n"
	if err := os.WriteFile(shellPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write shell: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var readyOnce sync.Once
	ready := make(chan struct{})
	daemon := headlessd.New(headlessd.Options{
		ConfigDir:       cfgDir,
		SessionID:       sessionID,
		Cols:            80,
		Rows:            24,
		Shell:           shellPath,
		Publish:         false,
		Logger:          pslog.NoopLogger(),
		ScrollbackLines: 1000,
	})
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				t.Fatalf("daemon run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("daemon did not stop")
		}
	})

	waitForLocalHeadlessSession(t, cfgDir, sessionID)
	h := newHarness(t)
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless",
		Cols:     80,
		Rows:     24,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: sessionID}}, nil
		},
		OnView: func(_ string, client *attach.Client) {
			orig := client.OnReady
			client.OnReady = func() {
				readyOnce.Do(func() {
					close(ready)
				})
				if orig != nil {
					orig()
				}
			}
		},
		SocketResolver: func(id string) (string, error) {
			if id != sessionID {
				return "", fmt.Errorf("unexpected session %q", id)
			}
			return headless.SocketPath(cfgDir, id)
		},
	})
	t.Cleanup(sess.Cancel)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for local attach view readiness:\n%s", sess.Screen().String())
	}
	sess.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.String(), "loading local headless") {
			return fmt.Errorf("loading banner remained after first snapshot:\n%s", screen.String())
		}
		return nil
	})
}

func waitForLocalHeadlessSession(t *testing.T, cfgDir, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	store := headless.NewStore(cfgDir)
	for time.Now().Before(deadline) {
		records, err := store.Reconcile()
		if err == nil {
			for _, rec := range records {
				if rec.SessionID == sessionID {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for local headless session %q", sessionID)
}
