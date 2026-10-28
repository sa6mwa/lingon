package clipboard

import (
	"context"
	"sync"

	"pkt.systems/lingon/internal/protocolpb"
)

// Handler assembles one transfer and submits delivery without waiting for PTY
// completion. The wire reader remains able to cancel, revoke or replace work.
type Handler struct {
	mu       sync.Mutex
	receiver Receiver
	active   *Delivery
	source   string
	id       string
	submit   func(*Delivery) error
}

// NewHandler constructs a transfer handler. Submit must admit work in input order
// without blocking; a full input queue returns an error rather than stalling I/O.
func NewHandler(submit func(*Delivery) error) *Handler { return &Handler{submit: submit} }

// Reset discards assembly and invalidates queued work on disconnect/control loss.
func (h *Handler) Reset() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.receiver.Reset()
	active := h.active
	h.active, h.id, h.source = nil, "", ""
	h.mu.Unlock()
	if active != nil {
		active.Cancel()
	}
}

// Handle consumes a wire chunk and emits ephemeral replies only to its sender.
// Replies may occur later from the ordered input worker, after actual delivery.
func (h *Handler) Handle(ctx context.Context, chunk *protocolpb.Clipboard, reply func(*protocolpb.Clipboard)) {
	h.mu.Lock()
	if chunk.Cancel {
		h.receiver.Cancel(chunk.Source, chunk.TransferId)
		var cancelled *Delivery
		if h.source == chunk.Source && h.id == chunk.TransferId {
			cancelled, h.active = h.active, nil
			h.source, h.id = "", ""
		}
		h.mu.Unlock()
		if cancelled != nil {
			cancelled.Cancel()
		}
		return
	}
	img, err := h.receiver.Accept(chunk)
	var previous *Delivery
	if err == nil && chunk.Offset == 0 {
		previous, h.active = h.active, nil
		h.source, h.id = "", ""
	}
	id, source := chunk.TransferId, chunk.Source
	respond := func(err error) {
		response := &protocolpb.Clipboard{TransferId: id, Source: source, Completed: true}
		if err != nil {
			response.Error = err.Error()
		}
		if reply != nil {
			reply(response)
		}
	}
	var delivery *Delivery
	if err == nil && len(img.Data) > 0 {
		delivery = NewDelivery(ctx, img, respond)
		h.active, h.source, h.id = delivery, chunk.Source, chunk.TransferId
	}
	h.mu.Unlock()
	if previous != nil {
		previous.Cancel()
	}
	if err != nil {
		respond(err)
	} else if delivery != nil {
		if err := h.submit(delivery); err != nil {
			delivery.finish(err)
		}
	} else if progress := Progress(chunk); progress != nil && reply != nil {
		reply(progress)
	}
}
