package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestWSConnSendPreservesFIFOOrder(t *testing.T) {
	framesCh := make(chan *protocolpb.Frame, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for i := 0; i < 3; i++ {
			frame, err := readFrame(r.Context(), conn, 1<<20)
			if err != nil {
				t.Errorf("read frame %d: %v", i, err)
				return
			}
			framesCh <- frame
		}
	}))
	t.Cleanup(server.Close)

	ctxDial, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDial()
	ws, _, err := websocket.Dial(ctxDial, server.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	conn := newWSConn("fifo-client", RoleClient, "fifo-session", ShareScopeControl, ws, nil)
	t.Cleanup(func() {
		_ = conn.Close(context.Background(), "done")
	})

	want := []*protocolpb.Frame{
		{SessionId: "fifo-session", Seq: 1, Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "first"}}},
		{SessionId: "fifo-session", Seq: 2, Payload: &protocolpb.Frame_Sessions{Sessions: &protocolpb.Sessions{Sessions: []*protocolpb.SessionInfo{{Id: "session-2", Name: "second"}}}}},
		{SessionId: "fifo-session", Seq: 3, Payload: &protocolpb.Frame_WallInactivityStatus{WallInactivityStatus: &protocolpb.WallInactivityStatus{Enabled: true, InactiveAfter: "2m"}}},
	}
	for _, frame := range want {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := conn.Send(ctx, frame); err != nil {
			cancel()
			t.Fatalf("send seq=%d: %v", frame.Seq, err)
		}
		cancel()
	}

	got := make([]*protocolpb.Frame, 0, len(want))
	deadline := time.After(2 * time.Second)
	for len(got) < len(want) {
		select {
		case frame := <-framesCh:
			got = append(got, frame)
		case <-deadline:
			t.Fatalf("timed out waiting for frames, got %d", len(got))
		}
	}

	for i := range want {
		if got[i].GetSeq() != want[i].GetSeq() {
			t.Fatalf("frame %d seq=%d, want %d", i, got[i].GetSeq(), want[i].GetSeq())
		}
		switch payload := want[i].Payload.(type) {
		case *protocolpb.Frame_Hello:
			if got[i].GetHello().GetClientId() != payload.Hello.GetClientId() {
				t.Fatalf("frame %d hello client=%q, want %q", i, got[i].GetHello().GetClientId(), payload.Hello.GetClientId())
			}
		case *protocolpb.Frame_Sessions:
			if len(got[i].GetSessions().GetSessions()) != 1 || got[i].GetSessions().GetSessions()[0].GetName() != payload.Sessions.GetSessions()[0].GetName() {
				t.Fatalf("frame %d sessions payload mismatch: %+v", i, got[i].GetSessions())
			}
		case *protocolpb.Frame_WallInactivityStatus:
			if got[i].GetWallInactivityStatus().GetInactiveAfter() != payload.WallInactivityStatus.GetInactiveAfter() {
				t.Fatalf("frame %d inactivity=%q, want %q", i, got[i].GetWallInactivityStatus().GetInactiveAfter(), payload.WallInactivityStatus.GetInactiveAfter())
			}
		default:
			t.Fatalf("unexpected payload type %T", payload)
		}
	}
}

func TestWSConnSendClosesWhenQueueFull(t *testing.T) {
	conn := &wsConn{
		sendCh: make(chan *protocolpb.Frame, 1),
		done:   make(chan struct{}),
	}
	conn.sendCh <- &protocolpb.Frame{Seq: 1}

	err := conn.Send(context.Background(), &protocolpb.Frame{Seq: 2})
	if err != errSendQueueFull {
		t.Fatalf("Send error = %v, want %v", err, errSendQueueFull)
	}
	select {
	case <-conn.done:
	default:
		t.Fatalf("connection was not closed after send queue overflow")
	}
}
