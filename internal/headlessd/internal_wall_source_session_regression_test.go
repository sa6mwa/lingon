package headlessd

import (
	"context"
	"io"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

func TestDaemonInternalWallPreservesSourceSessionID(t *testing.T) {
	cfgDir := t.TempDir()
	sessionID := "headless-target-session"
	sourceID := "headless-source-session"
	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	d := New(Options{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Publish:   false,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	waitUntil(t, 8*time.Second, func() bool {
		return headless.SocketExists(socketPath)
	})

	type routedWall struct {
		sessionID string
		message   string
	}
	wallFrames := make(chan routedWall, 8)

	attachClient := &attach.Client{
		Endpoint:       "local://headless",
		SessionID:      sessionID,
		UnixSocket:     socketPath,
		RequestControl: true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		NoHostTimeout:  8 * time.Second,
		Logger:         pslog.NoopLogger(),
		OnFrame: func(frame *protocolpb.Frame) {
			if frame == nil || frame.GetWall() == nil {
				return
			}
			select {
			case wallFrames <- routedWall{
				sessionID: frame.GetSessionId(),
				message:   frame.GetWall().GetMessage(),
			}:
			default:
			}
		},
	}
	ready := make(chan struct{})
	attachClient.OnReady = func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}

	attachCtx, cancelAttach := context.WithCancel(context.Background())
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- attachClient.RunDetached(attachCtx)
	}()
	select {
	case <-ready:
	case err := <-attachErr:
		t.Fatalf("attach exited early: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatalf("attach did not become ready")
	}

	const msg = "SOURCE_LOST connection lost to relay-x, reconnecting"
	if err := postInternalWallEvent(socketPath, internalWallEvent{
		SourceSessionID: sourceID,
		Sender:          headless.RoutedStatusSenderLost,
		Message:         msg,
		TimeoutSeconds:  2,
	}); err != nil {
		t.Fatalf("postInternalWallEvent: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	gotSource := ""
	for time.Now().Before(deadline) {
		select {
		case frame := <-wallFrames:
			if frame.message != msg {
				continue
			}
			gotSource = frame.sessionID
			break
		default:
			time.Sleep(20 * time.Millisecond)
		}
		if gotSource != "" {
			break
		}
	}
	if gotSource == "" {
		t.Fatalf("timed out waiting for routed wall frame")
	}
	if gotSource != sourceID {
		t.Fatalf("expected routed wall session_id=%q, got %q", sourceID, gotSource)
	}

	cancelAttach()
	select {
	case <-time.After(2 * time.Second):
	case <-attachErr:
	}
	cancelRun()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not stop")
	}
}
