package session

import (
	"context"
	"fmt"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
)

func (s *localSession) pasteImage(ctx context.Context, img clipboard.Image) error {
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	return s.pasteImageLocked(ctx, img)
}

func (s *localSession) pasteImageLocked(ctx context.Context, img clipboard.Image) error {
	if s.clipboardEndpoint == nil {
		return fmt.Errorf("session clipboard is not ready")
	}
	if err := s.clipboardEndpoint.Set(ctx, img); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.writePTYWithMode(ctx, []byte{0x16}, false)
	return err
}

// HandleSessionClipboard accepts an authenticated image chunk for a local PTY.
// Replies are ephemeral and sent only to the originating client. Completion may
// arrive asynchronously; this method never waits for PTY delivery.
func (r *Runner) HandleSessionClipboard(ctx context.Context, sessionID string, chunk *protocolpb.Clipboard, reply func(*protocolpb.Clipboard)) {
	local := r.localSession(r.resolveLocalSessionID(sessionID))
	if local == nil {
		if reply != nil {
			reply(&protocolpb.Clipboard{TransferId: chunk.TransferId, Source: chunk.Source, Completed: true, Error: "session not found"})
		}
		return
	}
	local.handleClipboard(ctx, chunk, reply)
}

func (s *localSession) handleClipboard(ctx context.Context, chunk *protocolpb.Clipboard, reply func(*protocolpb.Clipboard)) {
	s.clipboardHandler.Handle(ctx, chunk, reply)
}

type remoteInputOperation struct {
	data []byte
	run  func()
}

func (s *localSession) submitRemoteImage(d *clipboard.Delivery) error {
	op := remoteInputOperation{run: func() { _ = d.Run(s.pasteImage) }}
	select {
	case s.remoteInputCh <- op:
		return nil
	case <-d.Context().Done():
		return d.Context().Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return fmt.Errorf("image paste input queue is full; retry after pending input completes")
	}
}

// ResetSessionClipboard discards an upload when a local transport changes control.
func (r *Runner) ResetSessionClipboard(sessionID string) {
	if local := r.localSession(r.resolveLocalSessionID(sessionID)); local != nil {
		local.clipboardHandler.Reset()
	}
}
