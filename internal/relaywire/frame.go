package relaywire

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
)

// ReadFrame reads and unmarshals one relay protocol frame.
func ReadFrame(ctx context.Context, conn *websocket.Conn) (*protocolpb.Frame, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

// WriteFrame marshals and writes one relay protocol frame.
func WriteFrame(ctx context.Context, conn *websocket.Conn, frame *protocolpb.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// ActivityFrame builds a session activity notification frame.
func ActivityFrame(sessionID string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: sessionID,
		Payload:   &protocolpb.Frame_Activity{Activity: &protocolpb.Activity{}},
	}
}
