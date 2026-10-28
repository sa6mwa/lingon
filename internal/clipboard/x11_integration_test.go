package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.design/x/x11"
)

// This optional interoperability check starts its own X server. It never uses
// the test runner's DISPLAY, Xauthority, clipboard or desktop resources.
func startClipboardXvfb(ctx context.Context, t *testing.T) (string, string) {
	t.Helper()
	xvfb := os.Getenv("LINGON_TEST_XVFB")
	if xvfb == "" {
		var err error
		xvfb, err = exec.LookPath("Xvfb")
		if err != nil {
			t.Skip("Xvfb is required for X11 interoperability verification")
		}
	}
	auth := filepath.Join(t.TempDir(), "Xauthority")
	cookie := bytes.Repeat([]byte{42}, 16)
	data := []byte{255, 255}
	for _, field := range [][]byte{nil, nil, []byte("MIT-MAGIC-COOKIE-1"), cookie} {
		data = binary.BigEndian.AppendUint16(data, uint16(len(field)))
		data = append(data, field...)
	}
	if err := os.WriteFile(auth, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	cmd := exec.CommandContext(ctx, xvfb, "-displayfd", "3", "-screen", "0", "320x240x24", "-nolisten", "tcp", "-auth", auth, "-noreset")
	cmd.ExtraFiles = []*os.File{w}
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		t.Fatal(err)
	}
	_ = w.Close()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal("Xvfb readiness", err)
	}
	return ":" + strings.TrimSpace(line), auth
}

func TestClipboardX11INCRInteroperability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	display, auth := startClipboardXvfb(ctx, t)
	owner := NewX11(display, auth)
	defer owner.Close()
	reader := NewX11(display, auth)
	for _, mime := range []string{"image/png", "image/jpeg"} {
		img := testImage(t, mime, true)
		if err := owner.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		got, err := reader.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
			t.Fatal("INCR changed image bytes")
		}
	}
	img := testImage(t, "image/png", false)
	img.Data = append(img.Data, make([]byte, MaxBytes-len(img.Data))...)
	if err := owner.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, img.Data) {
		t.Fatal("maximum image truncated")
	}
	t.Run("closed owner preserves another selection", func(t *testing.T) {
		closed := NewX11(display, auth)
		_ = closed.Close()
		other := NewX11(display, auth)
		defer other.Close()
		img := testImage(t, "image/jpeg", false)
		if err := other.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		if err := closed.Set(ctx, testImage(t, "image/png", false)); err == nil {
			t.Fatal("closed clipboard accepted Set")
		}
		got, err := reader.Read(ctx)
		if err != nil || !bytes.Equal(got.Data, img.Data) {
			t.Fatal("closed owner changed another clipboard", err)
		}
	})
	t.Run("selection owns a snapshot", func(t *testing.T) {
		owner := NewX11(display, auth)
		defer owner.Close()
		img := testImage(t, "image/png", false)
		want := append([]byte(nil), img.Data...)
		if err := owner.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		img.Data[0] = 0
		got, err := reader.Read(ctx)
		if err != nil || !bytes.Equal(got.Data, want) {
			t.Fatal("caller mutated the published clipboard", err)
		}
	})
}

func destroyX11Reader(t *testing.T, c *xconn) {
	t.Helper()
	request := make([]byte, 8)
	request[0] = 4
	le.PutUint16(request[2:], 2)
	le.PutUint32(request[4:], c.win)
	if err := c.send(request); err != nil {
		t.Fatal(err)
	}
	// This roundtrip proves that Xvfb processed destruction and queued its
	// notification before a later reader's selection request.
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal(err)
	}
}

func TestClipboardX11DestroyedReaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	display, auth := startClipboardXvfb(ctx, t)
	owner := NewX11(display, auth)
	defer owner.Close()
	img := testImage(t, "image/png", true)
	if err := owner.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	reader, err := (&X11{Display: display, Authority: auth}).connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	query := make([]byte, 8) // QueryTree obtains this X server's root window ID.
	query[0] = 15
	le.PutUint16(query[2:], 2)
	le.PutUint32(query[4:], reader.win)
	root, err := reader.reply(query)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		property := reader.atoms["LINGON_CLIPBOARD"]
		if err := reader.send(x11.ConvertSelection(reader.win, reader.atoms["CLIPBOARD"], reader.atoms["image/png"], property, 0)); err != nil {
			t.Fatal(err)
		}
		for {
			p, err := reader.read()
			if err != nil {
				t.Fatal(err)
			}
			if p.EventCode() == x11.EventSelectionNotify {
				if p.SelectionNotify().Property != property {
					t.Fatalf("read %d rejected after destroyed readers", i+1)
				}
				break
			}
		}
		if _, incr, err := reader.readProperty(property, reader.atoms["image/png"]); err != nil || !incr {
			t.Fatalf("expected INCR handshake: %v, %v", incr, err)
		}
		destroyX11Reader(t, reader)
		reader.win++
		if err := reader.send(x11.CreateWindow(reader.win, le.Uint32(root.Raw[8:]))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewX11(display, auth).Read(ctx)
	if err != nil || !bytes.Equal(got.Data, img.Data) {
		t.Fatal("later reader could not retrieve the unchanged selection", err)
	}
}

type readerWriteGate struct {
	net.Conn
	opcode  byte
	window  uint32
	reached chan struct{}
	release chan struct{}
	once    sync.Once
	ctx     context.Context
}

func (g *readerWriteGate) Write(b []byte) (int, error) {
	if len(b) >= 8 && b[0] == g.opcode && le.Uint32(b[4:]) == g.window {
		g.once.Do(func() {
			close(g.reached)
			select {
			case <-g.release:
			case <-g.ctx.Done():
			}
		})
	}
	return g.Conn.Write(b)
}

func TestClipboardX11VanishedReaderWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	display, auth := startClipboardXvfb(ctx, t)
	for _, opcode := range []byte{2, 18, 25} {
		t.Run(map[byte]string{2: "subscribe", 18: "property", 25: "notify"}[opcode], func(t *testing.T) {
			reader, err := (&X11{Display: display, Authority: auth}).connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			owner, err := (&X11{Display: display, Authority: auth}).connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			if err := owner.send(x11.SetSelectionOwner(owner.win, owner.atoms["CLIPBOARD"], 0)); err != nil {
				t.Fatal(err)
			}
			if p, err := owner.reply(x11.GetSelectionOwner(owner.atoms["CLIPBOARD"])); err != nil || p.SelectionOwner() != owner.win {
				t.Fatal("failed to acquire test selection", err)
			}
			gate := &readerWriteGate{Conn: owner.net, opcode: opcode, window: reader.win, reached: make(chan struct{}), release: make(chan struct{}), ctx: ctx}
			owner.net = gate
			img := testImage(t, "image/png", true)
			done := make(chan struct{})
			go func() { defer close(done); owner.serveSelection(img) }()
			var release sync.Once
			defer func() { release.Do(func() { close(gate.release) }); _ = owner.Close(); <-done }()
			if err := reader.send(x11.ConvertSelection(reader.win, reader.atoms["CLIPBOARD"], reader.atoms["image/png"], reader.atoms["LINGON_CLIPBOARD"], 0)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.reached:
			case <-ctx.Done():
				t.Fatal("owner did not reach the requestor write")
			}
			destroyX11Reader(t, reader)
			release.Do(func() { close(gate.release) })
			got, err := NewX11(display, auth).Read(ctx)
			if err != nil || !bytes.Equal(got.Data, img.Data) {
				t.Fatal("vanished requestor dropped the X11 selection", err)
			}
		})
	}
}
