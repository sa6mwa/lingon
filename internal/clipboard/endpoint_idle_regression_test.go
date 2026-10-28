package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"golang.design/x/x11"
	"pkt.systems/lingon/internal/clock"
)

type endpointReadEvent struct {
	received uint64
}

// Translate server deadlines onto an injected clock while retaining real owned
// pipe I/O. Advancing mock time expires blocked reads/writes immediately; no
// wall-clock sleeps or polling are used to prove timeout behavior.
type endpointClockConn struct {
	net.Conn
	clock      *clock.MockClock
	mu         sync.Mutex
	timer      *clock.Timer
	generation uint64
	received   uint64
	reads      chan endpointReadEvent
	writes     chan uint64
}

func (c *endpointClockConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if err := c.Conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	if deadline.IsZero() {
		return nil
	}
	generation := c.generation
	c.timer = c.clock.AfterFunc(deadline.Sub(c.clock.Now()), func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if generation == c.generation {
			_ = c.Conn.SetDeadline(time.Unix(0, 1))
		}
	})
	return nil
}

func (c *endpointClockConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	event := endpointReadEvent{received: c.received}
	c.mu.Unlock()
	c.reads <- event
	n, err := c.Conn.Read(b)
	c.mu.Lock()
	c.received += uint64(n)
	c.mu.Unlock()
	return n, err
}

func (c *endpointClockConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if err == nil {
		c.mu.Lock()
		received := c.received
		c.mu.Unlock()
		c.writes <- received
	}
	return n, err
}

type endpointClockFixture struct {
	clock     *clock.MockClock
	server    *clipboardServer
	conn      *endpointClockConn
	client    net.Conn
	done      chan struct{}
	ctx       context.Context
	readBytes uint64
}

func newEndpointClockFixture(t *testing.T) *endpointClockFixture {
	t.Helper()
	server, client := net.Pipe()
	mock := clock.NewMock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	f := &endpointClockFixture{clock: mock, server: &clipboardServer{clock: mock, image: testImage(t, "image/png", false)}, conn: &endpointClockConn{Conn: server, clock: mock, reads: make(chan endpointReadEvent, 64), writes: make(chan uint64, 64)}, client: client, done: make(chan struct{}), ctx: ctx}
	copy(f.server.cookie[:], bytes.Repeat([]byte{42}, 16))
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() { defer close(f.done); defer server.Close(); f.server.serve(f.conn) }()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		select {
		case <-f.done:
		case <-ctx.Done():
			t.Error("owned X11 handler did not stop")
		}
		cancel()
	})
	return f
}

func (f *endpointClockFixture) waitRead(t *testing.T, received uint64) endpointReadEvent {
	t.Helper()
	for {
		select {
		case event := <-f.conn.reads:
			if event.received >= received {
				return event
			}
		case <-f.ctx.Done():
			t.Fatal("handler did not reach the expected read")
			return endpointReadEvent{}
		}
	}
}

func (f *endpointClockFixture) waitWrite(t *testing.T) uint64 {
	t.Helper()
	select {
	case received := <-f.conn.writes:
		return received
	case <-f.ctx.Done():
		t.Fatal("handler did not finish its reply")
		return 0
	}
}

func (f *endpointClockFixture) connect(t *testing.T) *xconn {
	t.Helper()
	if _, err := f.client.Write(x11.SetupRequest(x11.MITCookie, f.server.cookie[:])); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(f.client)
	setup, err := x11.ReadSetup(r)
	if err != nil {
		t.Fatal(err)
	}
	f.waitRead(t, f.waitWrite(t))
	c := &xconn{net: f.client, r: r, win: x11.NewIDGen(setup).Next(), atoms: make(map[string]uint32)}
	if err := c.send(x11.CreateWindow(c.win, setup.Root)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal(err)
	}
	f.readBytes = f.waitRead(t, f.waitWrite(t)).received
	return c
}

func (f *endpointClockFixture) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-f.ctx.Done():
		t.Fatal("incomplete X11 request did not time out")
	}
}

func TestClipboardEndpointIdleConnectionSurvives(t *testing.T) {
	f := newEndpointClockFixture(t)
	c := f.connect(t)
	// The handler has entered its next read. Advancing far beyond the old
	// deadline must not terminate an authenticated application connection.
	f.clock.Add(time.Hour)
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal("authenticated connection expired while idle", err)
	}
	selection := endpointAtom(t, c, "CLIPBOARD", false)
	target := endpointAtom(t, c, "image/png", false)
	property := endpointAtom(t, c, "AFTER_IDLE", false)
	endpointConvert(t, c, selection, target, property)
	got, _, err := c.readProperty(property, target)
	if err != nil || !bytes.Equal(got, f.server.image.Data) {
		t.Fatal("paste after inactivity failed", err)
	}
}

func TestClipboardEndpointIncompleteRequestTimeouts(t *testing.T) {
	for _, phase := range []string{"handshake", "header", "body"} {
		t.Run(phase, func(t *testing.T) {
			f := newEndpointClockFixture(t)
			if phase == "handshake" {
				f.waitRead(t, 0)
				f.clock.Add(10 * time.Second)
				f.waitClosed(t)
				return
			}
			f.connect(t)
			partial := []byte{43}
			if phase == "body" {
				partial = []byte{16, 0, 2, 0, 1}
			}
			if _, err := f.client.Write(partial); err != nil {
				t.Fatal(err)
			}
			f.waitRead(t, f.readBytes+uint64(len(partial)))
			f.clock.Add(30 * time.Second)
			f.waitClosed(t)
		})
	}
}
