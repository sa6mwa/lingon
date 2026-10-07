package publisher

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
)

// Observe the actual outgoing control frame, so the peer can send hello while
// ping is waiting for its pong. No sleep schedules this concurrency window.
type observedPingConn struct {
	net.Conn
	ping chan struct{}
	once sync.Once
}

func (c *observedPingConn) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	if n > 0 && data[0] == 0x89 {
		c.once.Do(func() { close(c.ping) })
	}
	return n, err
}

func TestPublisherSnapshotAndInputProceedDuringPendingPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		accepted <- conn
		<-ctx.Done()
	}))
	t.Cleanup(server.Close)
	// These real-time deadlines are deadlock guards; mock time initiates ping and
	// wire events establish the required hello-before-pong ordering.
	guard, stopGuard := context.WithTimeout(ctx, 5*time.Second)
	defer stopGuard()
	pingSent := make(chan struct{})
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &observedPingConn{Conn: conn, ping: pingSent}, nil
	}}
	defer transport.CloseIdleConnections()
	ws, _, err := websocket.Dial(guard, server.URL, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	var peer *websocket.Conn
	select {
	case peer = <-accepted:
	case <-guard.Done():
		t.Fatal("peer not accepted")
	}
	clk := &busyPingClock{MockClock: clock.NewMock(), ready: make(chan struct{})}
	clk.Add(time.Minute)
	p := New(Options{SessionID: "ping-snapshot", Clock: clk})
	snap := &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'A', 'B'}, Fg: []uint32{0, 0}, Bg: []uint32{0, 0}}
	p.Publish(nil, snap)
	p.setConn(ws)
	snapshotRequested := make(chan struct{}, 1)
	inputReceived := make(chan string, 1)
	p.OnFrame = func(frame *protocolpb.Frame) {
		if frame.GetSnapshot() != nil {
			snapshotRequested <- struct{}{}
		}
	}
	p.OnInput = func(data []byte) { inputReceived <- string(data) }
	readDone := make(chan error, 1)
	pingDone := make(chan error, 1)
	go func() { readDone <- p.readWS(ctx, ws) }()
	go func() { pingDone <- p.pingLoop(ctx, ws, nil) }()
	t.Cleanup(func() {
		cancel()
		_ = ws.CloseNow()
		_ = peer.CloseNow()
		for _, done := range []<-chan error{readDone, pingDone} {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("owned websocket loop did not stop")
			}
		}
	})
	select {
	case <-clk.ready:
	case <-guard.Done():
		t.Fatal("ping ticker not ready")
	}
	clk.Add(publisherPingInterval)
	select {
	case <-pingSent:
	case <-guard.Done():
		t.Fatal("mock tick did not send ping")
	}
	// The peer has not read ping yet, so pong cannot already have arrived.
	hello := &protocolpb.Frame{SessionId: "ping-snapshot", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{}}}
	if err := relaywire.WriteFrame(guard, peer, hello); err != nil {
		t.Fatal(err)
	}
	select {
	case <-snapshotRequested:
	case <-guard.Done():
		t.Fatal("hello did not request snapshot")
	}
	replyCtx, stopReply := context.WithTimeout(ctx, time.Second)
	defer stopReply()
	frame, err := relaywire.ReadFrame(replyCtx, peer)
	if err != nil {
		t.Fatalf("pending ping blocked the requested snapshot: %v", err)
	}
	if frame.SessionId != "ping-snapshot" || !proto.Equal(frame.GetSnapshot(), snap) {
		t.Fatalf("wrong snapshot: %v", frame)
	}
	const input = "INPUT_AFTER_PING"
	in := &protocolpb.Frame{SessionId: "ping-snapshot", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte(input)}}}
	if err := relaywire.WriteFrame(guard, peer, in); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inputReceived:
		if got != input {
			t.Fatalf("wrong input %q", got)
		}
	case <-guard.Done():
		t.Fatal("reader did not resume after snapshot/pong")
	}
	select {
	case err := <-pingDone:
		t.Fatalf("healthy transport stopped ping loop: %v", err)
	default:
	}
}
