package relayhost

import (
	"context"
	"errors"
	"fmt"
	"io"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/pty"
)

type inputOperation struct {
	data  []byte
	paste *clipboard.Delivery
}

func enqueuePaste(d *clipboard.Delivery, input chan<- inputOperation) error {
	select {
	case <-d.Context().Done():
		return d.Context().Err()
	case input <- inputOperation{paste: d}:
		return nil
	default:
		return fmt.Errorf("image paste input queue is full; retry after pending input completes")
	}
}

func enqueueInput(ctx context.Context, data []byte, input chan<- inputOperation, reset func()) error {
	select {
	case input <- inputOperation{data: data}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// If keyboard backpressure must pause the wire reader, invalidate any
		// queued image first rather than hiding a later cancel behind that wait.
		reset()
	}
	select {
	case input <- inputOperation{data: data}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Publication and the complete Ctrl+V write share the same ordered worker as
// keyboard input. Queue admission alone never acknowledges a successful paste.
func (h *Host) writePTY(ctx context.Context, writer pty.Writer, input <-chan inputOperation) {
	for {
		select {
		case <-ctx.Done():
			return
		case op, ok := <-input:
			if !ok {
				return
			}
			var err error
			if op.paste != nil {
				_ = op.paste.Run(func(pasteCtx context.Context, img clipboard.Image) error {
					if setErr := h.clipboardEndpoint.Set(pasteCtx, img); setErr != nil {
						return setErr
					}
					err = h.deliverInput(pasteCtx, writer, []byte{0x16})
					return err
				})
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					continue
				}
			} else {
				err = h.deliverInput(ctx, writer, op.data)
			}
			if err != nil {
				h.Logger.Debug("host.pty.write.failed", "err", err)
				return
			}
		}
	}
}

func (h *Host) deliverInput(ctx context.Context, writer pty.Writer, data []byte) error {
	err := writeInput(ctx, writer, data)
	if err == nil && len(data) > 0 && h.OnInput != nil {
		h.OnInput(append([]byte(nil), data...))
	}
	return err
}

func writeInput(ctx context.Context, writer pty.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := writer.Write(ctx, data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
