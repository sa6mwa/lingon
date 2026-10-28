package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.design/x/x11"
)

// Forward a test-owned X server behind a launchd-style socket. The pathname
// includes :0; it must not be interpreted as a TCP hostname or stripped away.
func xquartzSocket(ctx context.Context, t *testing.T, display, authority string) (string, string) {
	t.Helper()
	// macOS's default temporary root is long; leave room for sockaddr_un's
	// pathname limit without changing the process working directory or HOME.
	dir, err := os.MkdirTemp("", "lgx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "org.xquartz:0")
	backend, err := x11.ParseDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := x11.ParseXauthority(data)
	if err != nil || len(entries) == 0 {
		t.Fatal("missing fixture authority", err)
	}
	// The forwarding display has number zero but uses the backend's cookie.
	cookie := []byte{255, 255}
	for _, field := range [][]byte{nil, []byte("0"), []byte(x11.MITCookie), entries[0].Data} {
		cookie = binary.BigEndian.AppendUint16(cookie, uint16(len(field)))
		cookie = append(cookie, field...)
	}
	proxyAuthority := filepath.Join(dir, "Xauthority")
	if err := os.WriteFile(proxyAuthority, cookie, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	forwardX11(ctx, t, listener, backend)
	return path, proxyAuthority
}

func forwardX11(ctx context.Context, t *testing.T, listener net.Listener, backend x11.Display) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				var dialer net.Dialer
				d, err := dialer.DialContext(ctx, backend.Net, backend.Addr)
				if err != nil {
					return
				}
				defer d.Close()
				stop := context.AfterFunc(ctx, func() { _ = d.Close(); _ = client.Close() })
				defer stop()
				copied := make(chan struct{})
				go func() { _, _ = io.Copy(d, client); _ = d.Close(); close(copied) }()
				_, _ = io.Copy(client, d)
				_ = client.Close()
				<-copied
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); wg.Wait() })
}

func TestClipboardXQuartzSocketCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	path, authority := xquartzSocket(ctx, t, endpoint.Display, endpoint.Authority)
	for _, mime := range []string{"image/png", "image/jpeg"} {
		img := testImage(t, mime, false)
		if err := endpoint.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		for _, screen := range []string{"", ".1"} {
			got, err := NewX11(path+screen, authority).Read(ctx)
			if err != nil || got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
				t.Fatal("XQuartz socket capture failed", err)
			}
		}
	}
}

func TestClipboardXQuartzSocketPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	display, authority := startClipboardXvfb(ctx, t)
	path, cookie := xquartzSocket(ctx, t, display, authority)
	for _, screen := range []string{"", ".1"} {
		owner := NewX11(path+screen, cookie)
		for _, mime := range []string{"image/png", "image/jpeg"} {
			img := testImage(t, mime, true)
			if err := owner.Set(ctx, img); err != nil {
				_ = owner.Close()
				t.Fatal("XQuartz socket publication failed", err)
			}
			got, err := NewX11(display, authority).Read(ctx)
			if err != nil || got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
				_ = owner.Close()
				t.Fatal("socket publication changed the image", err)
			}
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClipboardDisplayAddresses(t *testing.T) {
	for _, tc := range []struct {
		display, network, address string
		number                    int
	}{
		{":7.1", "unix", "/tmp/.X11-unix/X7", 7},
		{"unix:2", "unix", "/tmp/.X11-unix/X2", 2},
		{"localhost:10.0", "tcp", "localhost:6010", 10},
		{"127.0.0.1:5", "tcp", "127.0.0.1:6005", 5},
		{"/private/tmp/com.apple.launchd.example/org.xquartz:0", "unix", "/private/tmp/com.apple.launchd.example/org.xquartz:0", 0},
		{"/var/run/com.apple.launchd.example/org.xquartz:12.2", "unix", "/var/run/com.apple.launchd.example/org.xquartz:12", 12},
		{"/tmp/launch.example/org.macosforge.xquartz:3", "unix", "/tmp/launch.example/org.macosforge.xquartz:3", 3},
	} {
		t.Run(tc.display, func(t *testing.T) {
			d, err := parseDisplay(tc.display)
			if err != nil || d.Net != tc.network || d.Addr != tc.address || d.Num != tc.number {
				t.Fatalf("display parsed incorrectly: %+v, %v", d, err)
			}
		})
	}
	for _, invalid := range []string{"", "/tmp/no-display-number", "/tmp/org.xquartz:bad", "localhost:bad"} {
		t.Run(fmt.Sprintf("invalid=%q", invalid), func(t *testing.T) {
			if _, err := parseDisplay(invalid); err == nil {
				t.Fatal("invalid display accepted")
			}
		})
	}
}
