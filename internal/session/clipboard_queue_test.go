package session

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
)

// This queue-only test has no session process, PTY or inherited stdio access.
func TestClipboardQueueBackpressureInvalidatesPaste(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	var pending *clipboard.Delivery
	h := clipboard.NewHandler(func(d *clipboard.Delivery) error { pending = d; return nil })
	h.Handle(ctx, &protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Source: "owner", Mime: "image/png", Total: uint32(encoded.Len()), Data: encoded.Bytes()}, nil)
	s := &localSession{ctx: ctx, clipboardHandler: h, remoteInputCh: make(chan remoteInputOperation, 1)}
	s.remoteInputCh <- remoteInputOperation{data: []byte("earlier")}
	done := make(chan error, 1)
	go func() { done <- s.enqueueRemoteInput([]byte("later")) }()
	select {
	case <-pending.Context().Done():
	case <-ctx.Done():
		t.Fatal("session input stalled before invalidating paste")
	}
	if string((<-s.remoteInputCh).data) != "earlier" {
		t.Fatal("earlier input lost")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("session input did not resume")
	}
	if string((<-s.remoteInputCh).data) != "later" {
		t.Fatal("later input reordered")
	}
}
