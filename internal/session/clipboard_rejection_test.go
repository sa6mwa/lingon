package session_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relaywire"
)

type sessionClipboardReaderFunc func(context.Context) (clipboard.Image, error)

func (f sessionClipboardReaderFunc) Read(ctx context.Context) (clipboard.Image, error) { return f(ctx) }

func clipboardRawShell(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "raw-clipboard-shell")
	script := "#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_TEST_CHILD=1 LINGON_TEST_OWN_PTY=1 LINGON_CLIPBOARD_RAW_KEYS=1\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run='^TestClipboardPTYHelper$'\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return shell
}

func TestClipboardNativeHostForwardsCtrlVWithoutCapture(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var captures atomic.Int32
	img := clipboardPTYImage(t, "image/png")
	reader := sessionClipboardReaderFunc(func(context.Context) (clipboard.Image, error) { captures.Add(1); return img, nil })
	h := ptytest.New(t)
	defer h.Close()
	host := h.StartHost(ptytest.HostOptions{SessionID: "native-clipboard", Shell: clipboardRawShell(t), Cols: 100, Rows: 24, ClipboardReader: reader, DisablePublish: true})
	defer host.Close()
	waitClipboardScreen(t, ctx, host, "CLIPBOARD_APP_READY")
	host.SendBytes([]byte{0x16})
	waitClipboardScreen(t, ctx, host, "KEY:16")
	if captures.Load() != 0 {
		t.Fatal("interactive host intercepted native Ctrl+V")
	}
	host.Send("z")
	waitClipboardScreen(t, ctx, host, "KEY:7a")
}

func TestClipboardHostRejectsMalformedImageBeforeInput(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	host := h.StartHost(ptytest.HostOptions{SessionID: "reject-image", Shell: clipboardRawShell(t), Cols: 100, Rows: 24})
	defer host.Close()
	waitClipboardScreen(t, ctx, host, "CLIPBOARD_APP_READY")
	waitClipboardRegistration(t, ctx, recorder, "reject-image")
	endpoint, err := relaywire.NormalizeEndpoint(h.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	tls, err := relaywire.ClientTLSConfig(h.TLSDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: tls}
	defer transport.CloseIdleConnections()
	ws, _, err := websocket.Dial(ctx, endpoint+"/ws/client", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.AccessToken()}}, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 20)
	if err := relaywire.WriteFrame(ctx, ws, &protocolpb.Frame{SessionId: "reject-image", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "malformed-sender", ClientType: "cli", WantsControl: true}}}); err != nil {
		t.Fatal(err)
	}
	for {
		frame, err := relaywire.ReadFrame(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if frame.GetWelcome() != nil {
			break
		}
	}
	img := clipboardPTYImage(t, "image/png")
	data := img.Data[:33]
	chunk := &protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Mime: "image/png", Total: uint32(len(data)), Data: data}
	if err := relaywire.WriteFrame(ctx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: chunk}}); err != nil {
		t.Fatal(err)
	}
	for {
		frame, err := relaywire.ReadFrame(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if reply := frame.GetClipboard(); reply != nil && reply.Completed {
			if reply.Error == "" {
				t.Fatal("host acknowledged an unusable image as successfully pasted")
			}
			break
		}
	}
	if err := relaywire.WriteFrame(ctx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("z")}}}); err != nil {
		t.Fatal(err)
	}
	waitClipboardScreen(t, ctx, host, "KEY:7a")
	if strings.Contains(host.Screen().String(), "KEY:16") {
		t.Fatal("malformed image delivered Ctrl+V")
	}
}
