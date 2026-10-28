package relayhost_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relayhost"
	"pkt.systems/lingon/internal/relaywire"
)

func TestSDKClipboardWireCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	initial, uploaded := deliveryImage(t, 1), deliveryImage(t, 2)
	if err := endpoint.Set(ctx, initial); err != nil {
		t.Fatal(err)
	}
	_, master, slave := ownedInputTerminal(t)
	input := relayhost.NewInputFixture(endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		input.Serve(ctx, ws)
	}))
	defer server.Close()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	defer cancel()
	send := func(frame *protocolpb.Frame) {
		t.Helper()
		if err := relaywire.WriteFrame(ctx, ws, frame); err != nil {
			t.Fatal(err)
		}
	}
	id := strings.Repeat("a", 32)
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: id, Source: "owner", Mime: uploaded.MIME, Total: uint32(len(uploaded.Data)), Data: uploaded.Data}}})
	deliver, err := input.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: id, Source: "owner", Cancel: true}}})
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("x")}}})
	// Admission of the later key proves that the preceding cancel was consumed
	// while image delivery was paused. No absence/timing assertion is used.
	key, err := input.Next(ctx)
	if err != nil {
		t.Fatal("wire reader blocked cancellation behind pending PTY delivery", err)
	}
	writer := &recordingInputWriter{Writer: master}
	deliver(writer)
	if writer.writes != 0 {
		t.Fatal("cancelled paste injected Ctrl+V")
	}
	got, err := clipboard.NewX11(endpoint.Display, endpoint.Authority).Read(ctx)
	if err != nil || !bytes.Equal(got.Data, initial.Data) {
		t.Fatal("cancelled paste replaced the clipboard", err)
	}
	key(writer)
	var b [1]byte
	if _, err := io.ReadFull(slave, b[:]); err != nil || b[0] != 'x' {
		t.Fatal("terminal input failed after cancellation", err)
	}
	fresh := deliveryImage(t, 3)
	nextID := strings.Repeat("b", 32)
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: nextID, Source: "owner", Mime: fresh.MIME, Total: uint32(len(fresh.Data)), Data: fresh.Data}}})
	next, err := input.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next(master)
	if _, err := io.ReadFull(slave, b[:]); err != nil || b[0] != 0x16 {
		t.Fatal("new paste failed after wire cancellation", err)
	}
	got, err = clipboard.NewX11(endpoint.Display, endpoint.Authority).Read(ctx)
	if err != nil || !bytes.Equal(got.Data, fresh.Data) {
		t.Fatal("new paste used stale cancelled image", err)
	}
	for {
		frame, err := relaywire.ReadFrame(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		if r := frame.GetClipboard(); r != nil && r.TransferId == nextID {
			if !r.Completed || r.Error != "" {
				t.Fatal("new paste was not acknowledged", r)
			}
			break
		}
	}
}
