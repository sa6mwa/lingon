package session_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"google.golang.org/protobuf/proto"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/headlessd"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayhost"
	"pkt.systems/pslog"
)

// This helper runs only as a child of ptytest's shell, with stdin/stdout wired
// exclusively to the test-owned child PTY. It never changes a caller terminal.
func TestClipboardPTYHelper(t *testing.T) {
	if os.Getenv("LINGON_CLIPBOARD_TEST_CHILD") != "1" {
		return
	}
	fmt.Print("\r\nCLIPBOARD_APP_READY\r\n")
	var key [1]byte
	for {
		if _, err := io.ReadFull(os.Stdin, key[:]); err != nil {
			os.Exit(0)
		}
		if key[0] != 0x16 || os.Getenv("LINGON_CLIPBOARD_RAW_KEYS") == "1" {
			fmt.Printf("KEY:%02x\n", key[0])
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var finishRead func() error
		if os.Getenv("LINGON_CLIPBOARD_JOIN_READER") == "1" {
			var err error
			finishRead, err = clipboardReaderShutdown(ctx)
			if err != nil {
				cancel()
				fmt.Printf("CLIPBOARD_ERROR:%s\n", err)
				continue
			}
		}
		img, err := clipboard.NewX11(os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY")).Read(ctx)
		if finishRead != nil {
			if shutdownErr := finishRead(); shutdownErr != nil {
				err = shutdownErr
			}
		}
		cancel()
		if err != nil {
			fmt.Printf("CLIPBOARD_ERROR:%s\n", err)
			continue
		}
		fmt.Printf("IMAGE:%x %s\n", sha256.Sum256(img.Data), img.MIME)
	}
}

func TestClipboardAttachPTYHelper(t *testing.T) {
	if os.Getenv("LINGON_CLIPBOARD_ATTACH_CHILD") != "1" {
		return
	}
	client := &attach.Client{Endpoint: os.Getenv("LINGON_CLIPBOARD_ENDPOINT"), AccessToken: os.Getenv("LINGON_CLIPBOARD_TOKEN"), TLSDir: os.Getenv("LINGON_CLIPBOARD_TLS"), SessionID: "tmux-image", Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, DisableSignalResize: true, DisableDesktopNotifications: true, Logger: pslog.NoopLogger()}
	if err := client.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClipboardThroughOwnedTMUX(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required for terminal interoperability")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_TEST_CHILD=1 LINGON_TEST_OWN_PTY=1 LINGON_CLIPBOARD_JOIN_READER=1\nexec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' -test.run='^TestClipboardPTYHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	host := h.StartHost(ptytest.HostOptions{SessionID: "tmux-image", Shell: shell, Cols: 110, Rows: 24})
	defer host.Close()
	waitClipboardScreen(t, ctx, host, "CLIPBOARD_APP_READY")
	waitClipboardRegistration(t, ctx, recorder, "tmux-image")
	source, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	terminal := startClipboardOwnedTMUX(t, ctx, h, source)
	defer terminal.Close()
	waitClipboardScreen(t, ctx, terminal, "CLIPBOARD_APP_READY")
	img := clipboardPTYImage(t, "image/png")
	if err := source.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	terminal.SendBytes([]byte{0x16})
	waitClipboardScreen(t, ctx, terminal, fmt.Sprintf("IMAGE:%x image/png", sha256.Sum256(img.Data)))
	terminal.Send("z")
	waitClipboardScreen(t, ctx, terminal, "KEY:7a")
	// Repeat with distinct bytes after teardown, then prove keyboard delivery.
	img = clipboardPTYImage(t, "image/jpeg")
	if err := source.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	terminal.SendBytes([]byte{0x16})
	waitClipboardScreen(t, ctx, terminal, fmt.Sprintf("IMAGE:%x image/jpeg", sha256.Sum256(img.Data)))
	terminal.Send("q")
	waitClipboardScreen(t, ctx, terminal, "KEY:71")
}

// Every process and terminal belongs to this fixture, including the tmux server.
func startClipboardOwnedTMUX(t *testing.T, ctx context.Context, h *ptytest.Harness, source *clipboard.Endpoint) *ptytest.PTYSession {
	t.Helper()
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	master, slave := ptytest.OpenPTY(t, 110, 24)
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	terminal := ptytest.NewPTYSession(t, master, slave, 110, 24)
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	cmd := exec.CommandContext(ctx, tmux, "-S", socket, "-f", "/dev/null", "new-session", "-s", "clipboard", "exec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' -test.run='^TestClipboardAttachPTYHelper$'")
	cmd.Env = append(os.Environ(), "LINGON_TEST_OWN_PTY=1", "LINGON_CLIPBOARD_ATTACH_CHILD=1", "LINGON_CLIPBOARD_ENDPOINT="+h.Endpoint(), "LINGON_CLIPBOARD_TOKEN="+h.AccessToken(), "LINGON_CLIPBOARD_TLS="+h.TLSDir(), "DISPLAY="+source.Display, "XAUTHORITY="+source.Authority, "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		_ = exec.CommandContext(cleanup, tmux, "-S", socket, "kill-server").Run()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return terminal
}

func TestClipboardTerminalToHeadlessPTY(t *testing.T) {
	// Both displays are owned by this test. No connection touches the desktop's
	// real X11 server or selection, even when this test is run inside a terminal.
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	reader := clipboard.NewX11(source.Display, source.Authority)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "clipboard-shell")
	script := "#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_TEST_CHILD=1 LINGON_TEST_OWN_PTY=1\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run='^TestClipboardPTYHelper$'\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"attach", "multi"} {
		t.Run(path, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
			defer h.Close()
			host := h.StartHost(ptytest.HostOptions{SessionID: "image", Shell: shell, Cols: 100, Rows: 24, ClipboardReader: reader})
			defer host.Close()
			waitClipboardScreen(t, ctx, host, "CLIPBOARD_APP_READY")
			waitClipboardRegistration(t, ctx, recorder, "image")
			terminal := host
			if path == "attach" {
				terminal = h.StartAttach(ptytest.AttachOptions{SessionID: "image", Cols: 100, Rows: 24, ClipboardReader: reader})
				defer terminal.Close()
				waitClipboardScreen(t, ctx, terminal, "CLIPBOARD_APP_READY")
			}
			if path == "multi" {
				terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "image", Cols: 100, Rows: 24, OnView: func(_ string, c *attach.Client) { c.ClipboardReader = reader }})
				defer terminal.Close()
				waitClipboardScreen(t, ctx, terminal, "CLIPBOARD_APP_READY")
			}
			for _, mime := range []string{"image/png", "image/jpeg"} {
				img := clipboardPTYImage(t, mime)
				if err := source.Set(ctx, img); err != nil {
					t.Fatal(err)
				}
				terminal.SendBytes([]byte{0x16})
				waitClipboardScreen(t, ctx, terminal, fmt.Sprintf("IMAGE:%x %s", sha256.Sum256(img.Data), mime))
			}
			if path == "attach" {
				img := clipboardPTYImage(t, "image/png")
				img.Data = append(img.Data, make([]byte, clipboard.MaxBytes-len(img.Data))...)
				if err := source.Set(ctx, img); err != nil {
					t.Fatal(err)
				}
				terminal.SendBytes([]byte{0x16})
				waitClipboardScreen(t, ctx, terminal, fmt.Sprintf("IMAGE:%x image/png", sha256.Sum256(img.Data)))
			}
			terminal.Send("z")
			waitClipboardScreen(t, ctx, terminal, "KEY:7a")
		})
	}
}

func TestClipboardLocalHeadlessTransport(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "clipboard-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_TEST_CHILD=1 LINGON_TEST_OWN_PTY=1\nexec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' -test.run='^TestClipboardPTYHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ready := make(chan headlessd.StartupReady, 1)
	d := headlessd.New(headlessd.Options{ConfigDir: t.TempDir(), SessionID: "image-local", Shell: shell, Cols: 100, Rows: 24, OnStartupReady: func(info headlessd.StartupReady) { ready <- info }})
	done := make(chan error, 1)
	go func() { defer close(done); done <- d.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	var info headlessd.StartupReady
	select {
	case info = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	source, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	h := ptytest.New(t)
	defer h.Close()
	client := h.StartAttach(ptytest.AttachOptions{SessionID: "image-local", UnixSocket: info.SocketPath, Cols: 100, Rows: 24, ClipboardReader: clipboard.NewX11(source.Display, source.Authority)})
	defer client.Close()
	waitClipboardScreen(t, ctx, client, "CLIPBOARD_APP_READY")
	img := clipboardPTYImage(t, "image/png")
	if err := source.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	client.SendBytes([]byte{0x16})
	waitClipboardScreen(t, ctx, client, fmt.Sprintf("IMAGE:%x image/png", sha256.Sum256(img.Data)))
}

func TestClipboardSDKHost(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(t.TempDir(), "clipboard-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_TEST_CHILD=1 LINGON_TEST_OWN_PTY=1\nexec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' -test.run='^TestClipboardPTYHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	// relayhost starts this command inside its own dedicated child PTY; no
	// caller stdio or controlling terminal is passed to the command.
	input := make(chan []byte, 1)
	host := &relayhost.Host{Endpoint: h.Endpoint(), Token: h.AccessToken(), TLSDir: h.TLSDir(), SessionID: "sdk-image", Command: []string{shell}, Cols: 100, Rows: 24, OnInput: func(data []byte) {
		select {
		case input <- data:
		default:
		}
	}}
	done := make(chan error, 1)
	go func() { defer close(done); done <- host.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitClipboardRegistration(t, ctx, recorder, "sdk-image")
	client := h.StartAttach(ptytest.AttachOptions{SessionID: "sdk-image", Cols: 100, Rows: 24})
	defer client.Close()
	waitClipboardScreen(t, ctx, client, "CLIPBOARD_APP_READY")
	// SendImage is transport-neutral: this represents a future non-X11 source.
	var img = clipboardPTYImage(t, "image/jpeg")
	attachClient := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), TLSDir: h.TLSDir(), SessionID: "sdk-image", Stdout: io.Discard, Stderr: io.Discard, DisableDesktopNotifications: true, DisableSignalResize: true}
	connected := make(chan struct{})
	attachClient.OnReady = func() { close(connected) }
	attachDone := make(chan error, 1)
	attachCtx, cancelAttach := context.WithCancel(ctx)
	go func() { defer close(attachDone); attachDone <- attachClient.RunDetached(attachCtx) }()
	attachClosed := false
	stopAttach := func() {
		if attachClosed {
			return
		}
		cancelAttach()
		if err := <-attachDone; err != nil {
			t.Error(err)
		}
		attachClosed = true
	}
	defer stopAttach()
	select {
	case <-connected:
	case err := <-attachDone:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := attachClient.SendImage(ctx, img); err != nil {
		t.Fatal(err)
	}
	waitClipboardScreen(t, ctx, client, fmt.Sprintf("IMAGE:%x image/jpeg", sha256.Sum256(img.Data)))
	select {
	case data := <-input:
		if !bytes.Equal(data, []byte{0x16}) {
			t.Fatal("image callback did not report Ctrl+V")
		}
	case <-ctx.Done():
		t.Fatal("input callback missing")
	}
	stopAttach()
	if err := ctx.Err(); err != nil {
		t.Fatalf("closing the sending attach canceled the host/observer: %v", err)
	}
	client.SendBytes([]byte{'z'})
	waitClipboardScreen(t, ctx, client, "KEY:7a")
}

// Run against an explicitly supplied adjacent Softline build for an additional
// interoperability check; ordinary tests use the self-contained PTY helper.
func TestClipboardSoftlineChat(t *testing.T) {
	chat := os.Getenv("LINGON_TEST_SOFTLINE_CHAT")
	if chat == "" {
		t.Skip("set LINGON_TEST_SOFTLINE_CHAT for Softline interoperability")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cache := t.TempDir()
	shell := filepath.Join(t.TempDir(), "softline-shell")
	script := fmt.Sprintf("#!/bin/sh\nexport XDG_CACHE_HOME='%s' SOFTLINE_HISTORY_DIR='%s' TERM=xterm-256color\nexec '%s'\n", cache, cache, strings.ReplaceAll(chat, "'", "'\\''"))
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	host := h.StartHost(ptytest.HostOptions{SessionID: "softline", Shell: shell, Cols: 140, Rows: 35})
	defer host.Close()
	waitClipboardRegistration(t, ctx, recorder, "softline")
	client := h.StartAttach(ptytest.AttachOptions{SessionID: "softline", Cols: 140, Rows: 35, ClipboardReader: clipboard.NewX11(source.Display, source.Authority)})
	defer client.Close()
	waitClipboardScreen(t, ctx, client, "streaming demo")
	for _, mime := range []string{"image/png", "image/jpeg"} {
		img := clipboardPTYImage(t, mime)
		if err := source.Set(ctx, img); err != nil {
			t.Fatal(err)
		}
		client.SendBytes([]byte{0x16})
		ext := ".png"
		if mime == "image/jpeg" {
			ext = ".jpeg"
		}
		waitClipboardScreen(t, ctx, client, ext)
		files, err := filepath.Glob(filepath.Join(cache, "softline", "*"+ext))
		if err != nil || len(files) != 1 {
			t.Fatalf("Softline did not create image: %v %v", files, err)
		}
		data, err := os.ReadFile(files[0])
		if err != nil || !bytes.Equal(data, img.Data) {
			t.Fatal("Softline saved different image bytes", err)
		}
	}
}

func clipboardPTYImage(t *testing.T, mime string) clipboard.Image {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	var state uint32 = 19
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			im.SetNRGBA(x, y, color.NRGBA{uint8(state), uint8(state >> 8), uint8(state >> 16), 255})
		}
	}
	var b bytes.Buffer
	var err error
	if mime == "image/png" {
		err = png.Encode(&b, im)
	} else {
		err = jpeg.Encode(&b, im, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return clipboard.Image{MIME: mime, Data: b.Bytes()}
}

func waitClipboardScreen(t *testing.T, ctx context.Context, terminal *ptytest.PTYSession, text string) {
	t.Helper()
	imageResult := strings.HasPrefix(text, "IMAGE:")
	if err := terminal.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
		return strings.Contains(screen.String(), text) || (imageResult && strings.Contains(screen.String(), "CLIPBOARD_ERROR:"))
	}); err != nil {
		t.Fatalf("waiting for %q: %v\n%s", text, err, terminal.Screen().String())
	}
	if imageResult && !strings.Contains(terminal.Screen().String(), text) {
		t.Fatalf("clipboard reader failed before %q:\n%s", text, terminal.Screen().String())
	}
}

func waitClipboardRegistration(t *testing.T, ctx context.Context, recorder *ptytest.WSRecorder, sessionID string) {
	t.Helper()
	if err := recorder.WaitForFrame(ctx, func(rec ptytest.FrameRecord) bool {
		if rec.Role != "host" || rec.Direction != ptytest.DirServerToClient {
			return false
		}
		var frame protocolpb.Frame
		if proto.Unmarshal(rec.Raw, &frame) != nil {
			return false
		}
		for _, s := range frame.GetSessions().GetSessions() {
			if s.Id == sessionID && s.Status == "active" {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatal("host registration not observed", err)
	}
}
