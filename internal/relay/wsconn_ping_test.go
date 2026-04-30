package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWSConnPingSkipsWhileWriteBusy(t *testing.T) {
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

	conn := &wsConn{conn: ws}
	conn.touchActivity()
	conn.lastActivity.Store(time.Now().Add(-time.Minute).UnixNano())
	conn.sendMu.Lock()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.Ping(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Ping err = %v, want nil while write busy", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("Ping blocked while write busy")
	}

	conn.sendMu.Unlock()
}
