package relayhost_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/relayhost"
)

type sdkRetryClock struct {
	*clock.MockClock
	attempted chan struct{}
}

func (c sdkRetryClock) NewTimer(d time.Duration) *clock.Timer {
	timer := c.MockClock.NewTimer(d)
	c.attempted <- struct{}{}
	return timer
}

// All descriptors belong to a private pipe. The registered retry timer proves
// EAGAIN occurred before cancellation; no sleeps or scheduler delay are assumed.
func TestSDKClipboardBackpressure(t *testing.T) {
	for _, action := range []string{"cancel", "revoke", "resume"} {
		t.Run(action, func(t *testing.T) {
			ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			endpoint, err := clipboard.NewEndpoint(ctx, "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer endpoint.Close()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			raw, err := writer.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			filled := 0
			if err := raw.Control(func(fd uintptr) {
				if err := unix.SetNonblock(int(fd), true); err != nil {
					t.Fatal(err)
				}
				for {
					n, err := unix.Write(int(fd), make([]byte, 4096))
					if n > 0 {
						filled += n
					}
					if errors.Is(err, unix.EAGAIN) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			mock := clock.NewMock()
			retry := sdkRetryClock{MockClock: mock, attempted: make(chan struct{}, 8)}
			input := relayhost.NewInputFixture(endpoint)
			input.SetClock(retry)
			pasteCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			result := make(chan error, 1)
			img := deliveryImage(t, 7)
			go func() { result <- input.Paste(pasteCtx, img) }()
			deliver, err := input.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			delivered := make(chan struct{})
			go func() { defer close(delivered); deliver(writer) }()
			select {
			case <-retry.attempted:
			case <-ctx.Done():
				t.Fatal("writer never reached backpressure", ctx.Err())
			}
			drain := func() {
				t.Helper()
				if _, err := io.CopyN(io.Discard, reader, int64(filled)); err != nil {
					t.Fatal(err)
				}
			}
			if action == "resume" {
				drain()
				mock.Add(2 * time.Millisecond)
			} else if action == "cancel" {
				cancel()
			} else {
				input.Revoke()
			}
			// Cancellation must finish while the pipe is still full and mock time has
			// not advanced. For resume, explicit mock advancement permits the retry.
			select {
			case <-delivered:
			case <-ctx.Done():
				t.Fatal("PTY worker did not finish", ctx.Err())
			}
			select {
			case err := <-result:
				if action == "resume" && err != nil || action != "resume" && !errors.Is(err, context.Canceled) {
					t.Fatalf("paste returned %v", err)
				}
			case <-ctx.Done():
				t.Fatal("paste result missing", ctx.Err())
			}
			want := byte(0x16)
			if action != "resume" {
				drain()
				want = 'z'
				if _, err := writer.Write([]byte{want}); err != nil {
					t.Fatal(err)
				}
			}
			var key [1]byte
			if _, err := io.ReadFull(reader, key[:]); err != nil || key[0] != want {
				t.Fatalf("next key=%x, want %02x, err=%v", key, want, err)
			}
		})
	}
}
