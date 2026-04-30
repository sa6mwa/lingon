package attach

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/clock"
)

func TestClientPingLoopSkipsWhileWriteBusy(t *testing.T) {
	oldPingInterval := clientPingInterval
	oldPingTimeout := clientPingTimeout
	clientPingInterval = 25 * time.Millisecond
	clientPingTimeout = 25 * time.Millisecond
	t.Cleanup(func() {
		clientPingInterval = oldPingInterval
		clientPingTimeout = oldPingTimeout
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

	c := &Client{Clock: clock.New()}
	c.lastActivity.Store(time.Now().Add(-time.Minute).UnixNano())
	c.writeMu.Lock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		c.pingLoop(ctx, ws, cancel)
		errCh <- c.error()
	}()

	select {
	case err := <-errCh:
		t.Fatalf("pingLoop returned while write busy: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	c.writeMu.Unlock()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("pingLoop err = %v, want nil after cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("pingLoop did not stop after cancel")
	}
}
