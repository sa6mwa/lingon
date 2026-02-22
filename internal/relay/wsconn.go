package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

type wsConn struct {
	id        string
	role      Role
	sessionID string
	scope     ShareScope
	conn      *websocket.Conn
	logger    pslog.Logger

	sendMu sync.Mutex
	sendCh chan *protocolpb.Frame
	ctrlCh chan *protocolpb.Frame
	done   chan struct{}
	closed sync.Once
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
		sendCh:    make(chan *protocolpb.Frame, 128),
		ctrlCh:    make(chan *protocolpb.Frame, 64),
		done:      make(chan struct{}),
	}
	go ws.writeLoop()
	return ws
}

func (c *wsConn) ID() string        { return c.id }
func (c *wsConn) Role() Role        { return c.role }
func (c *wsConn) Scope() ShareScope { return c.scope }
func (c *wsConn) SessionID() string { return c.sessionID }

func (c *wsConn) Send(ctx context.Context, frame *protocolpb.Frame) error {
	if frame == nil {
		return nil
	}
	ch := c.sendCh
	if isControlFrame(frame) {
		ch = c.ctrlCh
	}
	return c.enqueue(ctx, ch, frame)
}

func (c *wsConn) SendImmediate(ctx context.Context, frame *protocolpb.Frame) error {
	if frame == nil {
		return nil
	}
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageBinary, data)
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
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Ping(ctx)
}

func (c *wsConn) enqueue(ctx context.Context, ch chan *protocolpb.Frame, frame *protocolpb.Frame) error {
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	case <-ctx.Done():
		return ctx.Err()
	case ch <- frame:
		return nil
	}
}

func (c *wsConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}
		select {
		case frame := <-c.ctrlCh:
			if frame != nil {
				_ = c.writeFrame(frame)
			}
			continue
		default:
		}
		select {
		case <-c.done:
			return
		case frame := <-c.ctrlCh:
			if frame != nil {
				_ = c.writeFrame(frame)
			}
		case frame := <-c.sendCh:
			if frame != nil {
				_ = c.writeFrame(frame)
			}
		}
	}
}

func (c *wsConn) writeFrame(frame *protocolpb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Write(context.Background(), websocket.MessageBinary, data)
}

func isControlFrame(frame *protocolpb.Frame) bool {
	if frame == nil {
		return false
	}
	if frame.GetControl() != nil || frame.GetWelcome() != nil || frame.GetError() != nil || frame.GetSessions() != nil {
		return true
	}
	return false
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
