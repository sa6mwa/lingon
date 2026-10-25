package attach

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
)

type clipboardTimerClock struct {
	clock.Clock
	created chan struct{}
}

func (c *clipboardTimerClock) NewTimer(d time.Duration) *clock.Timer {
	timer := c.Clock.NewTimer(d)
	c.created <- struct{}{}
	return timer
}

func TestClipboardUploadWindowAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledge", true: "timeout"}[timeout], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			windows := make(chan *protocolpb.Clipboard, 4)
			cancelled := make(chan struct{}, 1)
			var received atomic.Uint64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				conn.SetReadLimit(1 << 20)
				for {
					frame, err := relaywire.ReadFrame(ctx, conn)
					if err != nil {
						return
					}
					chunk := frame.GetClipboard()
					if chunk == nil {
						return
					}
					if chunk.Cancel {
						cancelled <- struct{}{}
						continue
					}
					received.Add(uint64(len(chunk.Data)))
					end := chunk.Offset + uint32(len(chunk.Data))
					if end%clipboard.WindowBytes == 0 || end == chunk.Total {
						select {
						case windows <- chunk:
						case <-ctx.Done():
							return
						}
					}
				}
			}))
			defer server.Close()
			ws, _, err := websocket.Dial(ctx, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.CloseNow()
			mock := clock.NewMock()
			observed := &clipboardTimerClock{Clock: mock, created: make(chan struct{}, 8)}
			client := &Client{ws: ws, Clock: observed}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
				t.Fatal(err)
			}
			img := clipboard.Image{MIME: "image/png", Data: append(encoded.Bytes(), make([]byte, 2*clipboard.WindowBytes+1-encoded.Len())...)}
			done := make(chan error, 1)
			go func() { done <- client.SendImage(ctx, img) }()
			await := func() {
				t.Helper()
				select {
				case <-observed.created:
				case err := <-done:
					t.Fatal("sender returned before acknowledgement", err)
				case <-ctx.Done():
					t.Fatal("sender did not wait for acknowledgement")
				}
			}
			next := func() *protocolpb.Clipboard {
				t.Helper()
				select {
				case chunk := <-windows:
					return chunk
				case err := <-done:
					t.Fatal("sender returned before next window", err)
					return nil
				case <-ctx.Done():
					t.Fatal(ctx.Err())
					return nil
				}
			}
			first := next()
			await()
			if received.Load() != clipboard.WindowBytes {
				t.Fatal("sender exceeded unacknowledged window")
			}
			if timeout {
				mock.Add(15 * time.Second)
				select {
				case err := <-done:
					if err == nil || !strings.Contains(err.Error(), "timed out") {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case <-cancelled:
				case <-ctx.Done():
					t.Fatal("timed out upload not cancelled")
				}
				if received.Load() != clipboard.WindowBytes {
					t.Fatal("sender continued after timeout")
				}
				return
			}
			client.receiveClipboardReply(&protocolpb.Clipboard{TransferId: first.TransferId, Offset: clipboard.WindowBytes})
			second := next()
			await()
			if received.Load() != 2*clipboard.WindowBytes {
				t.Fatal("second window not bounded")
			}
			client.receiveClipboardReply(&protocolpb.Clipboard{TransferId: second.TransferId, Offset: 2 * clipboard.WindowBytes})
			last := next()
			await()
			client.receiveClipboardReply(&protocolpb.Clipboard{TransferId: last.TransferId, Completed: true})
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if received.Load() != uint64(len(img.Data)) {
				t.Fatal("upload truncated")
			}
		})
	}
}
