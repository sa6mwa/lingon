package relayhost

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/pslog"
)

// InputFixture exposes the real SDK writer to external tests, which can import
// ptytest without creating relayhost -> ptytest -> session -> relayhost cycles.
// No test hook or fixture surface is compiled into production binaries.
type InputFixture struct {
	host  *Host
	input chan inputOperation
	clock clock.Clock
}

// SetClock controls retry timers for the SDK's real file writer.
func (f *InputFixture) SetClock(c clock.Clock) { f.clock = c }

// Revoke cancels clipboard work through the same reset used on control changes.
func (f *InputFixture) Revoke() { f.host.clipboardHandler.Reset() }

type fixtureWriter struct{ io.Writer }

func (w fixtureWriter) Write(_ context.Context, data []byte) (int, error) {
	return w.Writer.Write(data)
}

// Serve runs the production SDK wire reader with the fixture's paused queue.
func (f *InputFixture) Serve(ctx context.Context, ws *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	frames := make(chan *protocolpb.Frame, 8)
	queue := relaywire.NewFrameQueue(0)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = f.host.writeFrames(ctx, ws, frames, queue) }()
	f.host.readWS(ctx, ws, nil, frames, f.input, queue, 0)
	cancel()
	wg.Wait()
}

// NewInputFixture creates a paused SDK input queue for owned-PTY tests.
func NewInputFixture(endpoint *clipboard.Endpoint) *InputFixture {
	f := &InputFixture{host: &Host{clipboardEndpoint: endpoint, Logger: pslog.NoopLogger()}, input: make(chan inputOperation, 8)}
	f.host.clipboardHandler = clipboard.NewHandler(func(d *clipboard.Delivery) error { return enqueuePaste(d, f.input) })
	return f
}

// Paste waits on the production admission/completion primitives for isolated
// writer tests. Production wire readers use Handler and never wait on the PTY.
func (f *InputFixture) Paste(ctx context.Context, img clipboard.Image) error {
	done := make(chan error, 1)
	err := clipboard.Transfer(ctx, img, func(ctx context.Context, chunk *protocolpb.Clipboard) error {
		chunk.Source = "fixture"
		f.host.clipboardHandler.Handle(ctx, chunk, func(reply *protocolpb.Clipboard) {
			if !reply.Completed {
				return
			}
			var err error
			switch reply.Error {
			case context.Canceled.Error():
				err = context.Canceled
			case context.DeadlineExceeded.Error():
				err = context.DeadlineExceeded
			case "":
			default:
				err = errors.New(reply.Error)
			}
			done <- err
		})
		return nil
	})
	if err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Next captures an admitted operation while its writer is paused. The returned
// closure resumes the production PTY writer for this one operation and joins it.
func (f *InputFixture) Next(ctx context.Context) (func(io.Writer), error) {
	select {
	case op := <-f.input:
		return func(writer io.Writer) {
			input := make(chan inputOperation, 1)
			input <- op
			close(input)
			var target pty.Writer = fixtureWriter{writer}
			if file, ok := writer.(*os.File); ok {
				target = pty.NewWriter(file, f.clock)
			}
			f.host.writePTY(ctx, target, input)
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
