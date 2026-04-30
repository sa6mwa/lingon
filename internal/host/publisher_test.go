package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestPublisherBufferOverflowResetsToSnapshot(t *testing.T) {
	p := NewPublisher(PublishOptions{
		SessionID:        "session",
		MaxReplayScreens: 1,
	})

	snap1 := &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'A', 'B'}, Fg: []uint32{0, 0}, Bg: []uint32{0, 0}}
	snap2 := &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'C', 'D'}, Fg: []uint32{0, 0}, Bg: []uint32{0, 0}}

	p.Publish([]byte("one\n"), snap1)
	p.Publish([]byte("two\n"), snap2)

	if p.outputQueue.Len() != 1 {
		t.Fatalf("queue size = %d, want 1", p.outputQueue.Len())
	}
	frame := p.outputQueue.Pop()
	if frame == nil || frame.GetSnapshot() == nil {
		t.Fatalf("expected snapshot after overflow")
	}
	if got := frame.GetSnapshot().GetRunes(); len(got) > 0 && got[0] != 'C' {
		t.Fatalf("expected latest snapshot after compaction")
	}
}

func TestPublisherPublishSendsActivityForRealOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var frame protocolpb.Frame
		if err := proto.Unmarshal(data, &frame); err != nil {
			t.Errorf("unmarshal activity frame: %v", err)
			return
		}
		if frame.GetActivity() == nil {
			t.Errorf("expected activity frame, got %#v", frame.Payload)
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	p := NewPublisher(PublishOptions{SessionID: "session"})
	p.setConn(ws)
	p.Publish([]byte("real-output"), &protocolpb.Snapshot{
		Cols: 2, Rows: 1, Runes: []uint32{'A', 'B'}, Fg: []uint32{0, 0}, Bg: []uint32{0, 0},
	})
}

func TestPublisherResizeDoesNotSendActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		readCtx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
		defer cancel()
		_, _, err = conn.Read(readCtx)
		if err == nil {
			t.Errorf("expected no frame for resize-only publish")
			return
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	p := NewPublisher(PublishOptions{SessionID: "session"})
	p.setConn(ws)
	p.Resize(120, 30, &protocolpb.Snapshot{
		Cols: 120, Rows: 30, Runes: make([]uint32, 120*30), Fg: make([]uint32, 120*30), Bg: make([]uint32, 120*30),
	})
}

func TestPublisherSendSnapshotDoesNotSendActivityOnReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var frame protocolpb.Frame
		if err := proto.Unmarshal(data, &frame); err != nil {
			t.Errorf("unmarshal replay snapshot: %v", err)
			return
		}
		if frame.GetSnapshot() == nil {
			t.Errorf("expected snapshot frame, got %#v", frame.Payload)
			return
		}
		if frame.GetActivity() != nil {
			t.Errorf("unexpected activity frame during replay snapshot")
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	p := NewPublisher(PublishOptions{SessionID: "session"})
	p.setConn(ws)
	p.mu.Lock()
	p.lastSnap = &protocolpb.Snapshot{
		Cols: 2, Rows: 1, Runes: []uint32{'A', 'B'}, Fg: []uint32{0, 0}, Bg: []uint32{0, 0},
	}
	p.mu.Unlock()
	p.sendSnapshot()
}

func TestPublisherConnectAndServeHonorsDialTimeout(t *testing.T) {
	old := publisherWSDialTimeout
	publisherWSDialTimeout = 120 * time.Millisecond
	t.Cleanup(func() {
		publisherWSDialTimeout = old
	})

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/host" {
			time.Sleep(2 * publisherWSDialTimeout)
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	p := NewPublisher(PublishOptions{
		Endpoint:  server.URL,
		Token:     "test-token",
		SessionID: "session",
		Insecure:  true,
	})

	start := time.Now()
	connected, err := p.connectAndServe(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected dial timeout error")
	}
	if connected {
		t.Fatalf("expected disconnected result")
	}
	if elapsed > 5*publisherWSDialTimeout {
		t.Fatalf("dial took too long: %v (timeout %v)", elapsed, publisherWSDialTimeout)
	}
}

func TestPublisherConnectAndServePingTimeout(t *testing.T) {
	oldPingInterval := publisherPingInterval
	oldPingTimeout := publisherPingTimeout
	publisherPingInterval = 25 * time.Millisecond
	publisherPingTimeout = 25 * time.Millisecond
	t.Cleanup(func() {
		publisherPingInterval = oldPingInterval
		publisherPingTimeout = oldPingTimeout
	})

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/host" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	p := NewPublisher(PublishOptions{
		Endpoint:  server.URL,
		Token:     "test-token",
		SessionID: "session",
		Insecure:  true,
	})

	start := time.Now()
	connected, err := p.connectAndServe(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected ping timeout error")
	}
	if !connected {
		t.Fatalf("expected connected result before ping timeout")
	}
	if elapsed > 8*time.Second {
		t.Fatalf("ping timeout took too long: %v", elapsed)
	}
}

func TestPublisherPingLoopSkipsWhileWriteBusy(t *testing.T) {
	oldPingInterval := publisherPingInterval
	oldPingTimeout := publisherPingTimeout
	publisherPingInterval = 25 * time.Millisecond
	publisherPingTimeout = 25 * time.Millisecond
	t.Cleanup(func() {
		publisherPingInterval = oldPingInterval
		publisherPingTimeout = oldPingTimeout
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		<-r.Context().Done()
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

	p := NewPublisher(PublishOptions{
		SessionID: "session",
	})
	p.lastActivity.Store(time.Now().Add(-time.Minute).UnixNano())
	p.writeMu.Lock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.pingLoop(ctx, ws, nil)
	}()

	select {
	case err := <-errCh:
		t.Fatalf("pingLoop returned while write busy: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	p.writeMu.Unlock()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("pingLoop err = %v, want nil after cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("pingLoop did not stop after cancel")
	}
}

func TestPublisherSessionRejectedGoesOfflineAndStopsReconnect(t *testing.T) {
	var connects int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/host" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt64(&connects, 1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{
			Message:         "session already has active host",
			SessionRejected: true,
		}}}
		data, err := proto.Marshal(frame)
		if err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageBinary, data)
	}))
	t.Cleanup(server.Close)

	p := NewPublisher(PublishOptions{
		Endpoint:  server.URL,
		Token:     "test-token",
		SessionID: "session",
		Insecure:  true,
		BackoffPolicy: &backoff.Policy{
			Base:   5 * time.Millisecond,
			Factor: 1.0,
			Max:    5 * time.Millisecond,
		},
	})
	rejected := make(chan string, 1)
	p.OnSessionRejected = func(message string) {
		select {
		case rejected <- message:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	select {
	case message := <-rejected:
		if !strings.Contains(message, "active host") {
			t.Fatalf("unexpected rejected message: %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected session rejected callback")
	}

	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt64(&connects); got != 1 {
		t.Fatalf("expected no reconnect after rejection, connects=%d", got)
	}
	if !p.Offline() {
		t.Fatalf("expected publisher to go offline after rejection")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("publisher run err = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("publisher did not stop after cancel")
	}
}

func TestPublisherNonRejectedServerErrorKeepsRetrying(t *testing.T) {
	var connects int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/host" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt64(&connects, 1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{
			Message: "temporary server error",
		}}}
		data, err := proto.Marshal(frame)
		if err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageBinary, data)
	}))
	t.Cleanup(server.Close)

	p := NewPublisher(PublishOptions{
		Endpoint:  server.URL,
		Token:     "test-token",
		SessionID: "session",
		Insecure:  true,
		BackoffPolicy: &backoff.Policy{
			Base:   5 * time.Millisecond,
			Factor: 1.0,
			Max:    5 * time.Millisecond,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&connects) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&connects); got < 2 {
		cancel()
		<-done
		t.Fatalf("expected reconnect attempts for non-rejected errors, connects=%d", got)
	}
	if p.Offline() {
		cancel()
		<-done
		t.Fatalf("expected publisher to stay online for non-rejected errors")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("publisher run err = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("publisher did not stop after cancel")
	}
}

func TestPublisherSendSessionClosedFrame(t *testing.T) {
	frames := make(chan *protocolpb.Frame, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/host" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			frame := &protocolpb.Frame{}
			if err := proto.Unmarshal(data, frame); err != nil {
				return
			}
			select {
			case frames <- frame:
			default:
			}
		}
	}))
	t.Cleanup(server.Close)

	p := NewPublisher(PublishOptions{
		Endpoint:  server.URL,
		Token:     "test-token",
		SessionID: "session-closed-test",
		Insecure:  true,
	})
	connected := make(chan struct{}, 1)
	p.OnStatus = func(isConnected bool, _ error) {
		if !isConnected {
			return
		}
		select {
		case connected <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("publisher did not connect")
	}

	p.SendSessionClosed("terminated")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-frames:
			if frame.GetSessionClosed() != nil {
				if reason := strings.TrimSpace(frame.GetSessionClosed().GetReason()); reason != "terminated" {
					t.Fatalf("session closed reason = %q, want %q", reason, "terminated")
				}
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("publisher run err = %v, want context canceled", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("publisher did not stop after cancel")
				}
				return
			}
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	cancel()
	t.Fatalf("did not receive session closed frame")
}

func TestPublisherDialHTTPClientReused(t *testing.T) {
	p := NewPublisher(PublishOptions{
		SessionID: "session",
		Token:     "token",
		Insecure:  true,
	})

	clientA, err := p.dialHTTPClient()
	if err != nil {
		t.Fatalf("first dialHTTPClient: %v", err)
	}
	clientB, err := p.dialHTTPClient()
	if err != nil {
		t.Fatalf("second dialHTTPClient: %v", err)
	}
	if clientA != clientB {
		t.Fatalf("expected dial HTTP client reuse")
	}

	p.closeHTTPClient()

	clientC, err := p.dialHTTPClient()
	if err != nil {
		t.Fatalf("third dialHTTPClient: %v", err)
	}
	if clientC == clientA {
		t.Fatalf("expected new client after close")
	}
}

func TestPublisherReconnectStressKeepsResourcesBounded(t *testing.T) {
	const (
		targetAttempts = int64(140)
		waitTimeout    = 6 * time.Second
	)

	baselineGoroutines := runtime.NumGoroutine()
	baselineThreadCreates := threadCreateProfileCount()

	p := NewPublisher(PublishOptions{
		Endpoint:  "https://127.0.0.1:1",
		Token:     "test-token",
		SessionID: "session",
		Insecure:  true,
		BackoffPolicy: &backoff.Policy{
			Base:   2 * time.Millisecond,
			Factor: 1.0,
			Max:    2 * time.Millisecond,
		},
	})

	var disconnects int64
	p.OnStatus = func(connected bool, _ error) {
		if !connected {
			atomic.AddInt64(&disconnects, 1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	deadline := time.Now().Add(waitTimeout)
	for atomic.LoadInt64(&disconnects) < targetAttempts && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	gotAttempts := atomic.LoadInt64(&disconnects)
	if gotAttempts < targetAttempts {
		cancel()
		<-done
		t.Fatalf("expected at least %d reconnect attempts, got %d", targetAttempts, gotAttempts)
	}

	currentGoroutines := runtime.NumGoroutine()
	if delta := currentGoroutines - baselineGoroutines; delta > 30 {
		cancel()
		<-done
		t.Fatalf("goroutine growth too high during reconnect stress: baseline=%d current=%d delta=%d", baselineGoroutines, currentGoroutines, delta)
	}

	currentThreadCreates := threadCreateProfileCount()
	if delta := currentThreadCreates - baselineThreadCreates; delta > 12 {
		cancel()
		<-done
		t.Fatalf("thread-create profile growth too high: baseline=%d current=%d delta=%d", baselineThreadCreates, currentThreadCreates, delta)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("publisher run err = %v, want context canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("publisher did not stop after cancel")
	}

	settleDeadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(settleDeadline) {
		if runtime.NumGoroutine() <= baselineGoroutines+15 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	finalGoroutines := runtime.NumGoroutine()
	t.Fatalf("goroutines did not settle after cancel: baseline=%d final=%d", baselineGoroutines, finalGoroutines)
}

func TestPublisherNormalizeReconnectDelay(t *testing.T) {
	p := NewPublisher(PublishOptions{
		SessionID: "session",
		BackoffPolicy: &backoff.Policy{
			Base:   250 * time.Millisecond,
			Factor: 2,
			Max:    5 * time.Second,
		},
	})

	if got := p.normalizeReconnectDelay(2 * time.Second); got != 2*time.Second {
		t.Fatalf("normalizeReconnectDelay(2s) = %v, want 2s", got)
	}
	if got := p.normalizeReconnectDelay(0); got != 250*time.Millisecond {
		t.Fatalf("normalizeReconnectDelay(0) = %v, want 250ms", got)
	}
	if got := p.normalizeReconnectDelay(-time.Second); got != 250*time.Millisecond {
		t.Fatalf("normalizeReconnectDelay(-1s) = %v, want 250ms", got)
	}
}

func TestPublisherNormalizeReconnectDelayFallsBackToDefaultBase(t *testing.T) {
	p := NewPublisher(PublishOptions{SessionID: "session"})
	p.backoffPolicy.Base = 0

	if got := p.normalizeReconnectDelay(0); got != backoff.DefaultPolicy.Base {
		t.Fatalf("normalizeReconnectDelay(0) = %v, want %v", got, backoff.DefaultPolicy.Base)
	}
}

func threadCreateProfileCount() int {
	n, _ := runtime.ThreadCreateProfile(nil)
	if n <= 0 {
		return 0
	}
	records := make([]runtime.StackRecord, n)
	n, ok := runtime.ThreadCreateProfile(records)
	if ok {
		return n
	}
	records = make([]runtime.StackRecord, n)
	n, _ = runtime.ThreadCreateProfile(records)
	return n
}
