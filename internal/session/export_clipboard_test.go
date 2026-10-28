package session

import (
	"context"
	"os"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/publisher"
)

// ClipboardTerminal contains only the sub-PTY descriptors owned by ptytest.
type ClipboardTerminal struct{ Master, Slave *os.File }

// ClipboardInputFixture pauses the real session queue without starting a shell
// or inheriting any caller terminal. Its publisher uses the production transport.
type ClipboardInputFixture struct{ local *localSession }

// HoldWriter simulates earlier input or a resize holding the PTY writer.
func (f *ClipboardInputFixture) HoldWriter() func() {
	if err := f.local.writeMu.Lock(f.local.ctx); err != nil {
		panic(err)
	}
	return f.local.writeMu.Unlock
}

// Paste runs the real clipboard publication and PTY delivery path.
func (f *ClipboardInputFixture) Paste(ctx context.Context, img clipboard.Image) error {
	return f.local.pasteImage(ctx, img)
}

// SetClock controls PTY retry time without wall-clock waits.
func (f *ClipboardInputFixture) SetClock(c clock.Clock) { f.local.clock = c }

// NewClipboardInputFixture binds the real session paste path to an owned PTY.
func NewClipboardInputFixture(ctx context.Context, endpoint *clipboard.Endpoint, tty ClipboardTerminal) *ClipboardInputFixture {
	s := &localSession{ctx: ctx, clock: clock.New(), clipboardEndpoint: endpoint,
		remoteInputCh: make(chan remoteInputOperation, 8), pty: tty.Master, tty: tty.Slave}
	s.clipboardHandler = clipboard.NewHandler(s.submitRemoteImage)
	return &ClipboardInputFixture{local: s}
}

// Publisher wires the same callbacks as Runner's published session setup.
func (f *ClipboardInputFixture) Publisher(opts publisher.Options) *publisher.Publisher {
	p := publisher.New(opts)
	p.OnClipboard = f.local.handleClipboard
	p.OnClipboardReset = f.local.clipboardHandler.Reset
	p.OnInput = func(data []byte) { _ = f.local.enqueueRemoteInput(data) }
	return p
}

// Next captures ordered work and returns its actual session-worker action.
func (f *ClipboardInputFixture) Next(ctx context.Context) (func(), error) {
	select {
	case op := <-f.local.remoteInputCh:
		return func() {
			if op.run != nil {
				op.run()
			} else {
				f.local.processRemoteInput(op.data)
			}
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
