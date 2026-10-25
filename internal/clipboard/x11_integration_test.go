package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This optional interoperability check starts its own X server. It never uses
// the test runner's DISPLAY, Xauthority, clipboard or desktop resources.
func TestClipboardX11INCRInteroperability(t *testing.T) {
	xvfb := os.Getenv("LINGON_TEST_XVFB")
	if xvfb == "" {
		var err error
		xvfb, err = exec.LookPath("Xvfb")
		if err != nil {
			t.Skip("Xvfb is required for X11 interoperability verification")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	defer r.Close()
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
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal("Xvfb readiness", err)
	}
	display := ":" + strings.TrimSpace(line)
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
