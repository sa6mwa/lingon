package clipboard

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
)

func testImage(t *testing.T, mime string, large bool) Image {
	t.Helper()
	n := 8
	if large {
		n = 512
	}
	i := image.NewNRGBA(image.Rect(0, 0, n, n))
	var state uint32 = 42
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			i.SetNRGBA(x, y, color.NRGBA{uint8(state), uint8(state >> 8), uint8(state >> 16), 255})
		}
	}
	var b bytes.Buffer
	var err error
	if mime == "image/png" {
		err = png.Encode(&b, i)
	} else {
		err = jpeg.Encode(&b, i, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return Image{MIME: mime, Data: b.Bytes()}
}

func TestClipboardTransferRoundTrip(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg"} {
		t.Run(mime, func(t *testing.T) {
			img := testImage(t, mime, true)
			var receiver Receiver
			var got Image
			chunks := 0
			err := Transfer(context.Background(), img, func(_ context.Context, c *protocolpb.Clipboard) error {
				chunks++
				if len(c.Data) > ChunkBytes {
					t.Fatal("unbounded chunk")
				}
				c.Source = "authenticated-source"
				var err error
				got, err = receiver.Accept(c)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if chunks < 2 || got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
				t.Fatal("image did not arrive unchanged in bounded chunks")
			}
		})
	}
}

func TestClipboardReceiverRejectsInvalidTransfers(t *testing.T) {
	img := testImage(t, "image/png", true)
	var chunks []*protocolpb.Clipboard
	if err := Transfer(context.Background(), img, func(_ context.Context, c *protocolpb.Clipboard) error {
		chunks = append(chunks, proto.Clone(c).(*protocolpb.Clipboard))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*protocolpb.Clipboard){
		"gap":             func(c *protocolpb.Clipboard) { c.Offset++ },
		"wrong source":    func(c *protocolpb.Clipboard) { c.Source = "attacker" },
		"wrong transfer":  func(c *protocolpb.Clipboard) { c.TransferId = strings.Repeat("a", 32) },
		"wrong size":      func(c *protocolpb.Clipboard) { c.Total++ },
		"oversized":       func(c *protocolpb.Clipboard) { c.Total = MaxBytes + 1 },
		"oversized chunk": func(c *protocolpb.Clipboard) { c.Data = make([]byte, ChunkBytes+1) },
		"wrong mime":      func(c *protocolpb.Clipboard) { c.Mime = "text/plain" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var receiver Receiver
			if _, err := receiver.Accept(chunks[0]); err != nil {
				t.Fatal(err)
			}
			bad := proto.Clone(chunks[1]).(*protocolpb.Clipboard)
			mutate(bad)
			if _, err := receiver.Accept(bad); err == nil {
				t.Fatal("invalid continuation accepted")
			}
			if _, err := receiver.Accept(chunks[1]); err == nil {
				t.Fatal("failed transfer retained state")
			}
		})
	}
	var receiver Receiver
	_, _ = receiver.Accept(chunks[0])
	receiver.Reset()
	if _, err := receiver.Accept(chunks[1]); err == nil {
		t.Fatal("reset retained transfer")
	}
}

func TestClipboardValidation(t *testing.T) {
	img := testImage(t, "image/png", false)
	for _, bad := range []Image{{MIME: "image/jpeg", Data: img.Data}, {MIME: "image/png", Data: []byte("not a PNG")}, {MIME: "text/plain", Data: img.Data}, {MIME: "image/png", Data: make([]byte, MaxBytes+1)}} {
		if bad.Validate() == nil {
			t.Fatal("invalid image accepted")
		}
	}
}

func TestClipboardCancelMatchesOnlyItsOwnTransfer(t *testing.T) {
	img := testImage(t, "image/png", true)
	var chunks []*protocolpb.Clipboard
	if err := Transfer(context.Background(), img, func(_ context.Context, c *protocolpb.Clipboard) error {
		c.Source = "current"
		chunks = append(chunks, proto.Clone(c).(*protocolpb.Clipboard))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var r Receiver
	_, _ = r.Accept(chunks[0])
	r.Cancel("previous", chunks[0].TransferId)
	r.Cancel("current", strings.Repeat("b", 32))
	var got Image
	for _, c := range chunks[1:] {
		var err error
		got, err = r.Accept(c)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(got.Data, img.Data) {
		t.Fatal("stale cancellation erased current image")
	}
	_, _ = r.Accept(chunks[0])
	r.Cancel("current", chunks[0].TransferId)
	if _, err := r.Accept(chunks[1]); err == nil {
		t.Fatal("cancelled transfer continued")
	}
}

type readerFunc func(context.Context) (Image, error)

func (f readerFunc) Read(ctx context.Context) (Image, error) { return f(ctx) }

func TestClipboardInputOrderAndBracketedPaste(t *testing.T) {
	img := testImage(t, "image/png", false)
	for split := 0; split <= len("\x1b[200~x\x16y\x1b[201~"); split++ {
		var events []string
		input := Input{Reader: readerFunc(func(context.Context) (Image, error) { events = append(events, "read"); return img, nil })}
		raw := func(b []byte) error { events = append(events, "raw:"+string(b)); return nil }
		paste := func(got Image) error {
			if !bytes.Equal(got.Data, img.Data) {
				t.Fatal("image changed")
			}
			events = append(events, "paste")
			return nil
		}
		if err := input.Forward(context.Background(), []byte("a\x16b"), raw, paste); err != nil {
			t.Fatal(err)
		}
		if strings.Join(events, "|") != "raw:a|read|paste|raw:b" {
			t.Fatalf("ordering: %q", events)
		}
		events = nil
		text := []byte("\x1b[200~x\x16y\x1b[201~")
		if err := input.Forward(context.Background(), text[:split], raw, paste); err != nil {
			t.Fatal(err)
		}
		if err := input.Forward(context.Background(), text[split:], raw, paste); err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event == "read" || event == "paste" {
				t.Fatal("Ctrl+V inside bracketed paste read clipboard")
			}
		}
	}
}

func TestClipboardInputNoImageAndFailure(t *testing.T) {
	var got []byte
	input := Input{Reader: readerFunc(func(context.Context) (Image, error) { return Image{}, nil })}
	raw := func(b []byte) error { got = append(got, b...); return nil }
	paste := func(Image) error { t.Fatal("unexpected image"); return nil }
	if err := input.Forward(context.Background(), []byte("a\x16b"), raw, paste); err != nil || string(got) != "a\x16b" {
		t.Fatal("ordinary Ctrl+V changed")
	}
	input.Reader = readerFunc(func(context.Context) (Image, error) { return Image{}, errors.New("clipboard failed") })
	if err := input.Forward(context.Background(), []byte{0x16}, raw, paste); err == nil {
		t.Fatal("clipboard error hidden")
	}
}

func TestClipboardHeadlessX11Endpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, err := NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	if mode, err := os.Stat(endpoint.Authority); err != nil || mode.Mode().Perm() != 0600 {
		t.Fatal("private authority permissions", err)
	}
	reader := NewX11(endpoint.Display, endpoint.Authority)
	if img, err := reader.Read(ctx); err != nil || len(img.Data) != 0 {
		t.Fatal("empty clipboard", err)
	}
	for _, mime := range []string{"image/png", "image/jpeg"} {
		img := testImage(t, mime, true)
		if err := endpoint.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		got, err := reader.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
			t.Fatal("headless clipboard bytes changed")
		}
	}
	bad := NewX11(endpoint.Display, t.TempDir()+"/no-authority")
	if _, err := bad.Read(ctx); err == nil {
		t.Fatal("unauthenticated client admitted")
	}
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(endpoint.Authority); !os.IsNotExist(err) {
		t.Fatal("authority left behind")
	}
	if _, err := reader.Read(ctx); err == nil {
		t.Fatal("endpoint survived close")
	}
}

func TestClipboardEndpointPreservesExistingDisplay(t *testing.T) {
	e, err := NewEndpoint(context.Background(), ":123", "/specific/authority")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Display != ":123" || e.Authority != "/specific/authority" {
		t.Fatal("inherited X11 environment replaced")
	}
}
