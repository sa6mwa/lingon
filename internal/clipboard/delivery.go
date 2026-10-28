package clipboard

import (
	"context"
	"fmt"
	"sync"
)

// Delivery owns one queued paste. Cancellation releases its image immediately,
// even if a stalled input worker cannot dequeue the operation yet.
type Delivery struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	stop      func() bool
	image     Image
	err       error
	started   bool
	finished  bool
	cancelled bool
	complete  func(error)
}

// NewDelivery creates cancellable, bounded work with an optional completion hook.
func NewDelivery(ctx context.Context, img Image, complete func(error)) *Delivery {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	d := &Delivery{ctx: ctx, cancel: cancel, image: img, complete: complete}
	d.mu.Lock()
	d.stop = context.AfterFunc(parent, d.Cancel)
	d.mu.Unlock()
	return d
}

// Context identifies the lifetime of this transfer, independently of its host.
func (d *Delivery) Context() context.Context { return d.ctx }

// Cancel invalidates pending work without waiting for input or completion I/O.
// Work already started observes cancellation through its context.
func (d *Delivery) Cancel() {
	d.mu.Lock()
	if d.finished || d.cancelled {
		d.mu.Unlock()
		return
	}
	d.cancelled = true
	d.cancel()
	d.image = Image{}
	d.mu.Unlock()
	d.stop()
}

// Run claims the image once at the ordered input boundary. Cancelled work never
// reaches deliver; callbacks and result publication occur after delivery returns.
func (d *Delivery) Run(deliver func(context.Context, Image) error) error {
	d.mu.Lock()
	if d.finished {
		err := d.err
		d.mu.Unlock()
		return err
	}
	if d.started {
		d.mu.Unlock()
		return fmt.Errorf("clipboard delivery already started")
	}
	d.started = true
	img := d.image
	d.image = Image{}
	d.mu.Unlock()
	err := d.ctx.Err()
	if err == nil {
		err = deliver(d.ctx, img)
	}
	return d.finish(err)
}

func (d *Delivery) finish(err error) error {
	d.mu.Lock()
	if d.finished {
		result := d.err
		d.mu.Unlock()
		return result
	}
	if err == nil {
		err = d.ctx.Err()
	}
	d.err, d.finished, d.image = err, true, Image{}
	d.mu.Unlock()
	d.stop()
	d.cancel()
	if d.complete != nil {
		d.complete(err)
	}
	return err
}
