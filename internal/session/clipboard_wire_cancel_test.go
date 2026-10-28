package session_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/publisher"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/lingon/internal/session"
)

func TestClipboardPublisherWireCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	initial := clipboardPTYImage(t, "image/png")
	uploaded := clipboardPTYImage(t, "image/jpeg")
	if err := endpoint.Set(ctx, initial); err != nil {
		t.Fatal(err)
	}
	master, slave := ptytest.OpenPTY(t, 100, 20)
	state, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	terminal := ptytest.NewPTYSession(t, master, slave, 100, 20)
	defer func() {
		terminal.Close()
		_ = term.Restore(int(slave.Fd()), state)
		_ = slave.Close()
		_ = master.Close()
	}()
	input := session.NewClipboardInputFixture(ctx, endpoint, session.ClipboardTerminal{Master: master, Slave: slave})
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	p := input.Publisher(publisher.Options{Endpoint: h.Endpoint(), Token: h.AccessToken(), TLSDir: h.TLSDir(), SessionID: "cancel-pub", Cols: 100, Rows: 20})
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	}()
	waitClipboardRegistration(t, ctx, recorder, "cancel-pub")
	base, err := relaywire.NormalizeEndpoint(h.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	tls, err := relaywire.ClientTLSConfig(h.TLSDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: tls}
	defer transport.CloseIdleConnections()
	ws, _, err := websocket.Dial(ctx, base+"/ws/client", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.AccessToken()}}, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	send := func(frame *protocolpb.Frame) {
		t.Helper()
		if err := relaywire.WriteFrame(ctx, ws, frame); err != nil {
			t.Fatal(err)
		}
	}
	send(&protocolpb.Frame{SessionId: "cancel-pub", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "cancel-owner", WantsControl: true}}})
	for {
		frame, err := relaywire.ReadFrame(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if frame.GetWelcome() != nil {
			break
		}
	}
	var id string
	if err := clipboard.Transfer(ctx, uploaded, func(ctx context.Context, c *protocolpb.Clipboard) error {
		id = c.TransferId
		return relaywire.WriteFrame(ctx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: c}})
	}); err != nil {
		t.Fatal(err)
	}
	deliver, err := input.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: id, Cancel: true}}})
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("x")}}})
	key, err := input.Next(ctx)
	if err != nil {
		t.Fatal("publisher blocked wire cancellation behind PTY delivery", err)
	}
	deliver()
	got, err := clipboard.NewX11(endpoint.Display, endpoint.Authority).Read(ctx)
	if err != nil || !bytes.Equal(got.Data, initial.Data) {
		t.Fatal("cancelled publisher paste changed clipboard", err)
	}
	key()
	var b [1]byte
	if _, err := io.ReadFull(slave, b[:]); err != nil || b[0] != 'x' {
		t.Fatal("cancelled publisher paste injected a key or stopped later input", err)
	}
}
