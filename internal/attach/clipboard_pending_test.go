package attach_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

type pendingClipboardReader struct {
	image    clipboard.Image
	captures atomic.Int32
}

func (r *pendingClipboardReader) Read(context.Context) (clipboard.Image, error) {
	r.captures.Add(1)
	return r.image, nil
}

// Only the shell started by ptytest enables this helper. All inherited helper
// stdio belongs to that test-owned child PTY, never the test runner's terminal.
func TestPendingClipboardChild(t *testing.T) {
	mode := os.Getenv("LINGON_PENDING_CLIPBOARD_CHILD")
	if mode == "" {
		return
	}
	if os.Getenv("LINGON_TEST_OWN_PTY") != "1" {
		t.Fatal("requires an owned child PTY")
	}
	fmt.Print("\r\nPENDING_APP_READY\r\n")
	var input []byte
	var key [1]byte
	for {
		if _, err := io.ReadFull(os.Stdin, key[:]); err != nil {
			return
		}
		input = append(input, key[0])
		if mode == "image" && key[0] == 0x16 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			img, err := clipboard.NewX11(os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY")).Read(ctx)
			cancel()
			if err != nil {
				fmt.Printf("\r\nIMAGE_ERROR:%v\r\n", err)
			} else {
				fmt.Printf("\r\nIMAGE:%x\r\n", sha256.Sum256(img.Data))
			}
		}
		if key[0] == 'z' {
			fmt.Printf("\r\nINPUT:%x\r\n", input)
			input = nil
		}
	}
}

func TestMultiAttachQueuedClipboardInput(t *testing.T) {
	for _, scenario := range []string{"image/connect", "image/reconnect", "bracketed/connect", "bracketed/reconnect"} {
		t.Run(scenario, func(t *testing.T) {
			mode, _, _ := strings.Cut(scenario, "/")
			reconnect := strings.HasSuffix(scenario, "/reconnect")
			t.Setenv("DISPLAY", "")
			t.Setenv("XAUTHORITY", "")
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			mock := clock.NewMock()
			h := ptytest.New(t, ptytest.WithClock(mock))
			defer h.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			shell := filepath.Join(t.TempDir(), "pending-shell")
			script := "#!/bin/sh\nstty raw -echo\nexport LINGON_TEST_OWN_PTY=1 LINGON_PENDING_CLIPBOARD_CHILD=" + mode + "\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run='^TestPendingClipboardChild$'\n"
			if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			published := make(chan struct{})
			var publishedOnce sync.Once
			host := h.StartHost(ptytest.HostOptions{SessionID: "pending-image", Shell: shell, Cols: 100, Rows: 30, Clock: mock, OnPublishStatus: func(status session.PublishStatus) {
				if status.Kind == session.PublishStatusConnected {
					publishedOnce.Do(func() { close(published) })
				}
			}})
			defer host.Close()
			await := func(event <-chan struct{}) {
				t.Helper()
				select {
				case <-event:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			await(published)
			if err := host.WaitForScreen(ctx, func(s ptytest.Screen) bool { return s.Contains("PENDING_APP_READY") }); err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
				t.Fatal(err)
			}
			// Cross a flow-control window: queued SendImage must receive progress and
			// completion acknowledgements while its OnReady callback is still draining.
			img := clipboard.Image{MIME: "image/png", Data: append(encoded.Bytes(), make([]byte, clipboard.WindowBytes+1)...)}
			reader := &pendingClipboardReader{image: img}
			reached, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			initialReady := make(chan struct{})
			var initialClient *attach.Client
			var viewCount atomic.Int32
			terminal := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "pending-image", Cols: 100, Rows: 30, Clock: mock, BackoffPolicy: backoff.Policy{Base: 10 * time.Millisecond, Max: 10 * time.Millisecond}, OnReconnect: func(_ string, _ int) { mock.Add(10 * time.Millisecond) }, OnView: func(_ string, client *attach.Client) {
				first := viewCount.Add(1) == 1
				client.ClipboardReader = reader
				ready := client.OnReady
				client.OnReady = func() {
					if reconnect && first {
						initialClient = client
						ready()
						close(initialReady)
						return
					}
					close(reached)
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
					ready()
					close(drained)
				}
			}})
			defer terminal.Close()
			if reconnect {
				await(initialReady)
				initialClient.Close("clipboard regression reconnect")
			}
			await(reached)
			queued := []byte{0x16, 'z'}
			if mode == "bracketed" {
				queued = []byte("\x1b[200~prefix")
			}
			terminal.SendBytes(append(append([]byte(nil), queued...), 0x0c, 'h'))
			// Help is rendered only after flushPending has enqueued the earlier bytes.
			// It is a positive queue-admission fence, independent of scheduler timing.
			if err := terminal.WaitForScreen(ctx, func(s ptytest.Screen) bool { return s.Contains("rename session") }); err != nil {
				t.Fatal("queued input was not consumed", err, terminal.Screen().String())
			}
			unblock()
			await(drained)
			terminal.SendBytes([]byte{'q'}) // Dismiss the help modal.
			expected := queued
			if mode == "bracketed" {
				tail := []byte("\x16suffix\x1b[201~z")
				terminal.SendBytes(tail)
				expected = append(append([]byte(nil), queued...), tail...)
			}
			marker := fmt.Sprintf("INPUT:%x", expected)
			if err := terminal.WaitForScreen(ctx, func(s ptytest.Screen) bool { return s.Contains(marker) }); err != nil {
				t.Fatal("input did not drain", err, terminal.Screen().String())
			}
			if mode == "image" {
				if reader.captures.Load() != 1 || !terminal.Screen().Contains(fmt.Sprintf("IMAGE:%x", sha256.Sum256(img.Data))) {
					t.Fatal("queued Ctrl+V bypassed image delivery", reader.captures.Load(), terminal.Screen().String())
				}
			} else if reader.captures.Load() != 0 {
				t.Fatal("bracketed queued/live input captured the clipboard", reader.captures.Load())
			}
		})
	}
}
