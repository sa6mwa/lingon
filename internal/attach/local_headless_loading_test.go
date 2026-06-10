package attach_test

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
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
