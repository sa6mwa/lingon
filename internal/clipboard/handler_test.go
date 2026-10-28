package clipboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func handlerChunk(t *testing.T, id, source string) *protocolpb.Clipboard {
	t.Helper()
	img := testImage(t, "image/png", false)
	return &protocolpb.Clipboard{TransferId: strings.Repeat(id, 32), Source: source, Mime: img.MIME, Total: uint32(len(img.Data)), Data: img.Data}
}

func deliveryRetainsImage(d *Delivery) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.image.Data != nil
}

func TestClipboardHandlerCancellationScope(t *testing.T) {
	for _, kind := range []string{"matching", "wrong source", "wrong id", "reset", "replacement", "parent"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queue := make(chan *Delivery, 4)
			replies := make(chan *protocolpb.Clipboard, 4)
			h := NewHandler(func(d *Delivery) error { queue <- d; return nil })
			chunk := handlerChunk(t, "a", "owner")
			h.Handle(ctx, chunk, func(reply *protocolpb.Clipboard) { replies <- reply })
			d := <-queue
			c := &protocolpb.Clipboard{TransferId: chunk.TransferId, Source: chunk.Source, Cancel: true}
			wantCancel := true
			switch kind {
			case "wrong source":
				c.Source = "other"
				wantCancel = false
				h.Handle(ctx, c, nil)
			case "wrong id":
				c.TransferId = strings.Repeat("b", 32)
				wantCancel = false
				h.Handle(ctx, c, nil)
			case "reset":
				h.Reset()
			case "replacement":
				h.Handle(ctx, handlerChunk(t, "b", "new-owner"), nil)
			case "parent":
				cancel()
			default:
				h.Handle(ctx, c, nil)
			}
			calls := 0
			err := d.Run(func(context.Context, Image) error { calls++; return nil })
			if wantCancel {
				if !errors.Is(err, context.Canceled) || calls != 0 {
					t.Fatal("cancelled work reached publication", err, calls)
				}
			} else if err != nil || calls != 1 {
				t.Fatal("unrelated cancellation invalidated work", err, calls)
			}
			r := <-replies
			if r.TransferId != chunk.TransferId || r.Source != chunk.Source || !r.Completed || len(r.Data) != 0 || (wantCancel != (r.Error != "")) {
				t.Fatal("completion escaped its transfer", r)
			}
			if deliveryRetainsImage(d) {
				t.Fatal("finished or cancelled work retained image bytes")
			}
			h.Reset()
		})
	}
}

func TestClipboardHandlerLateCancelAndBoundedImages(t *testing.T) {
	ctx := context.Background()
	queue := make(chan *Delivery, 128)
	h := NewHandler(func(d *Delivery) error { queue <- d; return nil })
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("%032d", i)
		c := handlerChunk(t, "a", "owner")
		c.TransferId = id
		h.Handle(ctx, c, nil)
		d := <-queue
		h.Handle(ctx, &protocolpb.Clipboard{TransferId: id, Source: "owner", Cancel: true}, nil)
		if deliveryRetainsImage(d) {
			t.Fatal("cancelled queued header retained a full image")
		}
	}
	latest := handlerChunk(t, "b", "new-owner")
	h.Handle(ctx, latest, nil)
	d := <-queue
	h.Handle(ctx, &protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Source: "owner", Cancel: true}, nil)
	calls := 0
	if err := d.Run(func(context.Context, Image) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("late cancellation erased new owner's work", err)
	}
	if err := d.Run(func(context.Context, Image) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("delivery executed twice", err)
	}
}

func TestClipboardHandlerBusyQueueAndDeliveryErrors(t *testing.T) {
	ctx := context.Background()
	busy := errors.New("queue full")
	var response *protocolpb.Clipboard
	h := NewHandler(func(*Delivery) error { return busy })
	h.Handle(ctx, handlerChunk(t, "a", "owner"), func(r *protocolpb.Clipboard) { response = r })
	if response == nil || response.Error != "queue full" || !response.Completed {
		t.Fatal("busy admission blocked or acknowledged success")
	}
	queue := make(chan *Delivery, 1)
	h = NewHandler(func(d *Delivery) error { queue <- d; return nil })
	h.Handle(ctx, handlerChunk(t, "b", "owner"), func(r *protocolpb.Clipboard) { response = r })
	d := <-queue
	failed := errors.New("PTY failed")
	if err := d.Run(func(context.Context, Image) error { return failed }); !errors.Is(err, failed) || response.Error != "PTY failed" {
		t.Fatal("delivery failure was acknowledged", err)
	}
}

func TestClipboardDeliveryCancellationContext(t *testing.T) {
	d := NewDelivery(context.Background(), testImage(t, "image/png", false), nil)
	d.Cancel()
	if err := d.Context().Err(); !errors.Is(err, context.Canceled) || deliveryRetainsImage(d) {
		t.Fatal("cancelled queued delivery retained its lifetime or image", err)
	}
}
