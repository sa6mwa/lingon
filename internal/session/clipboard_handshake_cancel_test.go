package session_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/pslog"
)

const handshakeFillerSize = 128 * 1024

// The daemon starts this helper exclusively on its own child PTY. The private
// TCP connection is a causal barrier: no child input is consumed until released.
func TestClipboardHandshakeChild(t *testing.T) {
	if os.Getenv("LINGON_CLIPBOARD_HANDSHAKE_CHILD") != "1" {
		return
	}
	if os.Getenv("LINGON_TEST_OWN_PTY") != "1" {
		t.Fatal("requires test-owned child PTY")
	}
	conn, err := net.Dial("tcp", os.Getenv("LINGON_CLIPBOARD_HANDSHAKE_ADDR"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	var key [1]byte
	if _, err := io.ReadFull(conn, key[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(io.Discard, os.Stdin, handshakeFillerSize); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(os.Stdin, key[:]); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("HANDSHAKE_KEY:%02x\n", key[0])
	if _, err := conn.Write(key[:]); err != nil {
		t.Fatal(err)
	}
	// Stay alive until the parent cancels the daemon and closes its owned PTY.
	_, _ = io.Copy(io.Discard, conn)
}

func TestClipboardLocalHandshakeCancelsQueuedPaste(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("LINGON_CLIPBOARD_HANDSHAKE_ADDR", listener.Addr().String())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "clipboard-shell")
	script := "#!/bin/sh\nstty raw -echo\nexport LINGON_CLIPBOARD_HANDSHAKE_CHILD=1 LINGON_TEST_OWN_PTY=1\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run='^TestClipboardHandshakeChild$'\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ready := make(chan headlessd.StartupReady, 1)
	daemon := headlessd.New(headlessd.Options{ConfigDir: t.TempDir(), SessionID: "handshake-image", Shell: shell, Cols: 80, Rows: 24, Logger: pslog.NoopLogger(), OnStartupReady: func(info headlessd.StartupReady) { ready <- info }})
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	var info headlessd.StartupReady
	select {
	case info = <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	accepted := make(chan net.Conn, 1)
	go func() { conn, _ := listener.Accept(); accepted <- conn }()
	var child net.Conn
	select {
	case child = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if child == nil {
		t.Fatal("no child barrier")
	}
	defer child.Close()
	_ = child.SetDeadline(time.Now().Add(10 * time.Second))
	var key [1]byte
	if _, err := io.ReadFull(child, key[:]); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", info.SocketPath)
	}}
	defer transport.CloseIdleConnections()
	dial := func(id string, wants bool) *websocket.Conn {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, "ws://local/ws/client", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ws.CloseNow() })
		if err := relaywire.WriteFrame(ctx, ws, &protocolpb.Frame{SessionId: info.SessionID, Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: id, WantsControl: wants}}}); err != nil {
			t.Fatal(err)
		}
		welcomed := false
		for {
			frame, err := relaywire.ReadFrame(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			if welcome := frame.GetWelcome(); welcome != nil {
				if wants && welcome.HolderClientId != id {
					t.Fatal("wrong holder")
				}
				welcomed = true
			}
			if welcomed && frame.GetSnapshot() != nil {
				break
			}
		}
		return ws
	}
	owner := dial("old-owner", true)
	send := func(ws *websocket.Conn, frame *protocolpb.Frame) {
		t.Helper()
		if err := relaywire.WriteFrame(ctx, ws, frame); err != nil {
			t.Fatal(err)
		}
	}
	send(owner, &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: bytes.Repeat([]byte("a"), handshakeFillerSize)}}})
	image := clipboardPTYImage(t, "image/png")
	if err := clipboard.Transfer(ctx, image, func(ctx context.Context, chunk *protocolpb.Clipboard) error {
		return relaywire.WriteFrame(ctx, owner, &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: chunk}})
	}); err != nil {
		t.Fatal(err)
	}
	// Resync follows upload in the same reader, proving queue admission while the
	// child and the earlier oversized input remain blocked.
	send(owner, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{}}})
	for {
		frame, err := relaywire.ReadFrame(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if frame.GetSnapshot() != nil {
			break
		}
	}
	next := dial("new-owner", true)
	// This input must not be needed to invalidate the old paste: the hello itself
	// changed ownership, and setHolder now sees an unchanged holder.
	send(next, &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("z")}}})
	if _, err := child.Write([]byte("g")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(child, key[:]); err != nil || key[0] != 'z' {
		t.Fatalf("revoked paste delivered key %x, err=%v", key, err)
	}
}
