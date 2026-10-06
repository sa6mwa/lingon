package publisher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clock"
)

// Queue a ticker event before the loop's select. Cancellation is already ready,
// so the loop must handle either selected branch without issuing a failed ping.
type queuedPingClock struct{ *clock.MockClock }

func (c *queuedPingClock) NewTicker(d time.Duration) *clock.Ticker {
	t := c.MockClock.NewTicker(d)
	c.Add(d)
	return t
}

type busyPingClock struct {
	*clock.MockClock
	ready    chan struct{}
	observed chan struct{}
}

func (c *busyPingClock) NewTicker(d time.Duration) *clock.Ticker {
	t := c.MockClock.NewTicker(d)
	close(c.ready)
	return t
}
func (c *busyPingClock) Now() time.Time {
	if c.observed != nil {
		select {
		case c.observed <- struct{}{}:
		default:
		}
	}
	return c.MockClock.Now()
}

func TestPublisherCanceledQueuedPingReturnsWithoutReconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDial()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	for range 20 {
		clk := &queuedPingClock{MockClock: clock.NewMock()}
		clk.Add(time.Minute)
		p := New(Options{SessionID: "canceled", Clock: clk})
		p.lastActivity.Store(clk.Now().Add(-time.Minute / 2).UnixNano())
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		reconnectCanceled := false
		if err := p.pingLoop(cctx, ws, func() { reconnectCanceled = true }); err != nil {
			t.Fatalf("intentional cancellation returned a ping error: %v", err)
		}
		if reconnectCanceled {
			t.Fatal("intentional cancellation triggered failure recovery")
		}
	}
}
