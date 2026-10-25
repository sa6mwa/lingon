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
	_, err := s.writePTY([]byte{0x16})
	return err
}

// HandleSessionClipboard accepts an authenticated image chunk for a local PTY.
// Replies are ephemeral and must be returned only to the originating client.
func (r *Runner) HandleSessionClipboard(ctx context.Context, sessionID string, chunk *protocolpb.Clipboard) *protocolpb.Clipboard {
	local := r.localSession(r.resolveLocalSessionID(sessionID))
	if local == nil {
		return &protocolpb.Clipboard{TransferId: chunk.TransferId, Source: chunk.Source, Completed: true, Error: "session not found"}
	}
	return local.handleClipboard(ctx, chunk)
}

func (s *localSession) handleClipboard(ctx context.Context, chunk *protocolpb.Clipboard) *protocolpb.Clipboard {
	s.clipboardMu.Lock()
	if chunk.Cancel {
		s.clipboardReceiver.Cancel(chunk.Source, chunk.TransferId)
		s.clipboardMu.Unlock()
		return nil
	}
	img, err := s.clipboardReceiver.Accept(chunk)
	s.clipboardMu.Unlock()
	if err == nil && len(img.Data) == 0 {
		return clipboard.Progress(chunk)
	}
	if err == nil {
		err = s.enqueueRemoteImage(ctx, img)
	}
	reply := &protocolpb.Clipboard{TransferId: chunk.TransferId, Source: chunk.Source, Completed: true}
	if err != nil {
		reply.Error = err.Error()
	}
	return reply
}

type remoteInputOperation struct {
	data  []byte
	paste func() error
	done  chan error
}

func (s *localSession) enqueueRemoteImage(ctx context.Context, img clipboard.Image) error {
	op := remoteInputOperation{paste: func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.pasteImage(ctx, img)
	}, done: make(chan error, 1)}
	select {
	case s.remoteInputCh <- op:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	select {
	case err := <-op.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// ResetSessionClipboard discards an upload when a local transport changes control.
func (r *Runner) ResetSessionClipboard(sessionID string) {
	if local := r.localSession(r.resolveLocalSessionID(sessionID)); local != nil {
		local.clipboardReceiver.Reset()
	}
}
