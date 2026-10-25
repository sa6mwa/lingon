package relay

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
)

func (h *Hub) handleClipboard(ctx context.Context, conn connection, frame *protocolpb.Frame) error {
	c := frame.GetClipboard()
	if c.Completed || c.Error != "" || len(c.TransferId) != 32 || len(c.Data) > clipboard.ChunkBytes || c.Total > clipboard.MaxBytes || (!c.Cancel && (len(c.Data) == 0 || c.Total == 0 || uint64(c.Offset)+uint64(len(c.Data)) > uint64(c.Total) || (c.Mime != "image/png" && c.Mime != "image/jpeg"))) {
		return fmt.Errorf("invalid clipboard request")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[conn.SessionID()]
	if s == nil || s.host == nil || s.clients[conn.ID()] != conn {
		return fmt.Errorf("clipboard client is no longer registered")
	}
	if _, pending := s.pendingClients[conn.ID()]; pending {
		return fmt.Errorf("clipboard handshake incomplete")
	}
	if conn.Scope() != ShareScopeControl {
		return fmt.Errorf("control not permitted")
	}
	if (c.Offset != 0 || c.Cancel) && s.controller != conn.ID() {
		return fmt.Errorf("clipboard control changed")
	}
	// Enqueue while holding the authorization lock: revocation/unregistration
	// cannot cross the credential check and enqueue boundary.
	if s.controller != conn.ID() {
		s.controller = conn.ID()
		holder := s.clientIDs[conn.ID()]
		if holder == "" {
			holder = conn.ID()
		}
		ctrl := frameControl(conn.SessionID(), holder)
		if err := s.host.Send(ctx, ctrl); err != nil {
			return err
		}
		for id, client := range s.clients {
			if _, pending := s.pendingClients[id]; !pending {
				_ = client.Send(ctx, ctrl)
			}
		}
	}
	forward := proto.Clone(frame).(*protocolpb.Frame)
	forward.SessionId = conn.SessionID()
	forward.Seq = 0
	forward.GetClipboard().Source = conn.ID()
	if c.Offset == 0 && !c.Cancel {
		s.clipboardID = c.TransferId
		s.clipboardSource = conn.ID()
	}
	if c.Cancel && s.clipboardID == c.TransferId && s.clipboardSource == conn.ID() {
		s.clipboardID = ""
		s.clipboardSource = ""
	}
	return s.host.Send(ctx, forward)
}

func (h *Hub) cancelClipboardLocked(s *sessionState, source string) {
	if s.host == nil || s.clipboardSource != source || s.clipboardID == "" {
		return
	}
	_ = s.host.Send(context.Background(), &protocolpb.Frame{SessionId: s.id, Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: s.clipboardID, Source: source, Cancel: true}}})
	s.clipboardID = ""
	s.clipboardSource = ""
}
