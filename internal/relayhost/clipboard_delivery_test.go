package relayhost_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayhost"
)

func deliveryImage(t *testing.T, shade uint8) clipboard.Image {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	im.SetNRGBA(0, 0, color.NRGBA{R: shade, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return clipboard.Image{MIME: "image/png", Data: b.Bytes()}
}

type recordingInputWriter struct {
	io.Writer
	writes int
}

func (w *recordingInputWriter) Write(data []byte) (int, error) {
	w.writes++
	return w.Writer.Write(data)
}

func TestSDKClipboardQueuedCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	initial := deliveryImage(t, 1)
	if err := endpoint.Set(ctx, initial); err != nil {
		t.Fatal(err)
	}
	_, master, _ := ownedInputTerminal(t)
	writer := &recordingInputWriter{Writer: master}
	input := relayhost.NewInputFixture(endpoint)
	pasteCtx, abort := context.WithCancel(ctx)
	done := make(chan error, 1)
	img := deliveryImage(t, 2)
	go func() { done <- input.Paste(pasteCtx, img) }()
	deliver, err := input.Next(ctx)
	if err != nil {
		abort()
		t.Fatal(err)
	}
	abort()
	deliver(writer)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled queued paste did not fail", err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled queued paste did not finish")
	}
	got, err := clipboard.NewX11(endpoint.Display, endpoint.Authority).Read(ctx)
	if err != nil || !bytes.Equal(got.Data, initial.Data) || writer.writes != 0 {
		t.Fatal("cancelled queued paste changed clipboard or sent input", err)
	}
}

func ownedInputTerminal(t *testing.T) (*ptytest.PTYSession, *os.File, *os.File) {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, 100, 20)
	state, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	terminal := ptytest.NewPTYSession(t, master, slave, 100, 20)
	t.Cleanup(func() {
		terminal.Close()
		_ = term.Restore(int(slave.Fd()), state)
		_ = slave.Close()
		_ = master.Close()
	})
	return terminal, master, slave
}

func TestSDKClipboardPublicationAndPTYDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	initial := deliveryImage(t, 1)
	if err := endpoint.Set(ctx, initial); err != nil {
		t.Fatal(err)
	}
	terminal, master, slave := ownedInputTerminal(t)
	input := relayhost.NewInputFixture(endpoint)
	reader := clipboard.NewX11(endpoint.Display, endpoint.Authority)
	for _, shade := range []uint8{2, 3} {
		img := deliveryImage(t, shade)
		done := make(chan error, 1)
		go func() { done <- input.Paste(ctx, img) }()
		deliver, err := input.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// The operation is queued behind a paused writer. It must not change
		// the clipboard that an earlier queued paste could still consume.
		got, err := reader.Read(ctx)
		if err != nil || !bytes.Equal(got.Data, initial.Data) {
			t.Error("queued upload replaced the clipboard before PTY delivery", err)
		}
		appDone := make(chan error, 1)
		go func() {
			var key [1]byte
			if _, err := io.ReadFull(slave, key[:]); err != nil {
				appDone <- err
				return
			}
			got, err := reader.Read(ctx)
			if err == nil && (key[0] != 0x16 || !bytes.Equal(got.Data, img.Data)) {
				err = fmt.Errorf("application received key %#x with wrong image", key[0])
			}
			if err == nil {
				_, err = fmt.Fprintf(slave, "\r\nPASTED:%x\r\n", sha256.Sum256(got.Data))
			}
			appDone <- err
		}()
		deliver(master)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("paste did not acknowledge its delivered key")
		}
		select {
		case err := <-appDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("application did not receive the paste key")
		}
		want := fmt.Sprintf("PASTED:%x", sha256.Sum256(img.Data))
		if err := terminal.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return strings.Contains(screen.String(), want) }); err != nil {
			t.Fatal(err)
		}
		initial = img
	}
}

func TestSDKClipboardFailedPTYWriteIsNotAcknowledged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	terminal, master, _ := ownedInputTerminal(t)
	input := relayhost.NewInputFixture(endpoint)
	done := make(chan error, 1)
	img := deliveryImage(t, 2)
	go func() { done <- input.Paste(ctx, img) }()
	deliver, err := input.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	terminal.Close()   // Join the screen reader before closing its descriptor.
	_ = master.Close() // Only this test's owned PTY can fail.
	deliver(master)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed PTY write received a successful paste acknowledgement")
		}
	case <-ctx.Done():
		t.Fatal("failed PTY write did not return a result")
	}
}
