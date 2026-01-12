package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

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
}

func newWSConn(id string, role Role, sessionID string, scope ShareScope, conn *websocket.Conn, logger pslog.Logger) *wsConn {
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	return &wsConn{id: id, role: role, sessionID: sessionID, scope: scope, conn: conn, logger: logger}
}

func (c *wsConn) ID() string        { return c.id }
func (c *wsConn) Role() Role        { return c.role }
func (c *wsConn) Scope() ShareScope { return c.scope }
func (c *wsConn) SessionID() string { return c.sessionID }

func (c *wsConn) Send(ctx context.Context, frame *protocolpb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageBinary, data)
}

func (c *wsConn) Close(ctx context.Context, reason string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Close(websocket.StatusNormalClosure, reason)
}

func (c *wsConn) Ping(ctx context.Context) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.conn.Ping(ctx)
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
