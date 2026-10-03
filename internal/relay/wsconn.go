package relay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

const wsWriteTimeout = 5 * time.Second

var errSendQueueFull = errors.New("connection send queue full")

// A replay occupies one FIFO slot. Its immutable frames are already bounded by
// the hub's history budget. A connection retains at most one active replay and
// one queued replay, including when a peer sends another hello during delivery.
type outboundFrames struct {
	frame  *protocolpb.Frame
	replay []*protocolpb.Frame
}

type wsConn struct {
	id         string
	role       Role
	sessionID  string
	scope      ShareScope
	shareToken string
	conn       *websocket.Conn
	logger     pslog.Logger

	sendMu       sync.Mutex
	lastActivity atomic.Int64
	sendCh       chan outboundFrames
	replayQueued atomic.Bool
	done         chan struct{}
	closed       sync.Once
}

func newWSConn(id string, role Role, sessionID string, scope ShareScope, conn *websocket.Conn, logger pslog.Logger) *wsConn {
	if logger == nil {
		logger = logging.Default()
	}
	ws := &wsConn{
		id:        id,
		role:      role,
		sessionID: sessionID,
		scope:     scope,
		conn:      conn,
		logger:    logger,
		sendCh:    make(chan outboundFrames, 128),
		done:      make(chan struct{}),
	}
	ws.touchActivity()
	go ws.writeLoop()
	return ws
}

func (c *wsConn) ID() string         { return c.id }
func (c *wsConn) Role() Role         { return c.role }
func (c *wsConn) Scope() ShareScope  { return c.scope }
func (c *wsConn) SessionID() string  { return c.sessionID }
func (c *wsConn) ShareToken() string { return c.shareToken }

func (c *wsConn) Send(ctx context.Context, frame *protocolpb.Frame) error {
	if frame == nil {
		return nil
	}
	return c.enqueue(ctx, outboundFrames{frame: frame})
}

// SendReplay transfers a bounded, immutable history slice to the FIFO writer.
// Admission never waits for transport I/O while the hub holds its replay lock.
func (c *wsConn) SendReplay(ctx context.Context, frames []*protocolpb.Frame) error {
	if len(frames) == 0 {
		return nil
	}
	if !c.replayQueued.CompareAndSwap(false, true) {
		c.closeNow()
		return errSendQueueFull
	}
	if err := c.enqueue(ctx, outboundFrames{replay: frames}); err != nil {
		c.replayQueued.Store(false)
		return err
	}
	return nil
}

func (c *wsConn) SendImmediate(ctx context.Context, frame *protocolpb.Frame) error {
	if frame == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return err
	}
	c.touchActivity()
	return nil
}

func (c *wsConn) Close(ctx context.Context, reason string) error {
	c.closed.Do(func() {
		close(c.done)
	})
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Close(websocket.StatusNormalClosure, reason)
}

func (c *wsConn) Ping(ctx context.Context) error {
	return c.PingIfIdle(ctx, 0)
}

func (c *wsConn) PingIfIdle(ctx context.Context, minIdle time.Duration) error {
	if minIdle > 0 && c.idleFor() < minIdle {
		return nil
	}
	if !c.sendMu.TryLock() {
		return nil
	}
	defer c.sendMu.Unlock()
	if minIdle > 0 && c.idleFor() < minIdle {
		return nil
	}
	err := c.conn.Ping(ctx)
	if err == nil {
		c.touchActivity()
	}
	return err
}

func (c *wsConn) touchActivity() {
	if c == nil {
		return
	}
	c.lastActivity.Store(time.Now().UTC().UnixNano())
}

func (c *wsConn) idleFor() time.Duration {
	if c == nil {
		return 0
	}
	nanos := c.lastActivity.Load()
	if nanos <= 0 {
		return 0
	}
	last := time.Unix(0, nanos)
	now := time.Now().UTC()
	if now.After(last) {
		return now.Sub(last)
	}
	return 0
}

func (c *wsConn) enqueue(ctx context.Context, frames outboundFrames) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Prefer cancellation over accepting more output on a closed connection.
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	case <-ctx.Done():
		return ctx.Err()
	case c.sendCh <- frames:
		return nil
	default:
		c.closeNow()
		return errSendQueueFull
	}
}

func (c *wsConn) closeNow() {
	c.closed.Do(func() {
		close(c.done)
	})
	if c.conn != nil {
		_ = c.conn.CloseNow()
	}
}

func (c *wsConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case frames := <-c.sendCh:
			if frames.replay != nil {
				c.replayQueued.Store(false)
			}
			if err := c.writeFrames(frames); err != nil {
				c.closeNow()
				return
			}
		}
	}
}

func (c *wsConn) writeFrames(frames outboundFrames) error {
	if frames.frame != nil {
		return c.writeFrame(frames.frame)
	}
	for _, frame := range frames.replay {
		select {
		case <-c.done:
			return fmt.Errorf("connection closed")
		default:
		}
		if err := c.writeFrame(frame); err != nil {
			return err
		}
	}
	return nil
}

func (c *wsConn) writeFrame(frame *protocolpb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	defer cancel()
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		return err
	}
	c.touchActivity()
	return nil
}

func readFrame(ctx context.Context, conn *websocket.Conn, readLimit int64) (*protocolpb.Frame, error) {
	conn.SetReadLimit(readLimit)
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if msgType != websocket.MessageBinary {
		return nil, fmt.Errorf("expected binary websocket frame")
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

// writeFrame is kept for future use when we introduce buffered writes.
// nolint:unused
func writeFrame(ctx context.Context, conn *wsConn, frame *protocolpb.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Send(ctx, frame)
}
