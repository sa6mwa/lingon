package attach_test

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachStartupDoesNotSendResizeToRelayHost(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(rec))

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-startup",
		SessionName: "attach-camera-startup",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-startup"})
	host.Send("echo ATTACH_CAMERA_STARTUP_READY\n")
	if !screenContainsWithin(host, "ATTACH_CAMERA_STARTUP_READY", 2*time.Second) {
		t.Fatalf("expected host readiness marker before attach startup")
	}

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-camera-startup",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attach.Cancel)

	if !screenContainsWithin(attach, "ATTACH_CAMERA_STARTUP_READY", 3*time.Second) {
		t.Fatalf("expected attach to render host session on startup")
	}
	waitForClientCount(t, h, "attach-camera-startup", 1, 3*time.Second)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	for _, frame := range rec.Frames() {
		if frame.Role == "client" && frame.Direction == ptytest.DirClientToServer && frame.SessionID == "attach-camera-startup" && frame.Payload == "resize" {
			t.Fatalf("unexpected resize frame during multi-attach startup: %+v", frame)
		}
	}
}

func TestMultiAttachResizeDoesNotResizeRelayHostPTY(t *testing.T) {
	h := newHarness(t)

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-resize",
		SessionName: "attach-camera-resize",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-resize"})
	host.Send("echo ATTACH_CAMERA_RESIZE_READY\n")
	if !screenContainsWithin(host, "ATTACH_CAMERA_RESIZE_READY", 2*time.Second) {
		t.Fatalf("expected host readiness marker before attach")
	}

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-camera-resize",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attach.Cancel)

	if !screenContainsWithin(attach, "ATTACH_CAMERA_RESIZE_READY", 3*time.Second) {
		t.Fatalf("expected attach to render host session before resize")
	}

	host.Send("stty size; echo SIZE0_DONE\n")
	if !screenContainsWithin(host, "SIZE0_DONE", 2*time.Second) {
		t.Fatalf("expected baseline size marker on host")
	}
	if !screenContainsWithin(host, "40 120", 2*time.Second) {
		t.Fatalf("expected relay-backed host PTY size to remain 120x40 before attach resize, got:\n%s", host.Screen().String())
	}

	attach.Resize(60, 16)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	host.Send("stty size; echo SIZE1_DONE\n")
	if !screenContainsWithin(host, "SIZE1_DONE", 2*time.Second) {
		t.Fatalf("expected post-resize size marker on host")
	}
	if !screenContainsWithin(host, "40 120", 2*time.Second) {
		t.Fatalf("expected multi-attach viewport resize not to resize relay-backed host PTY, got:\n%s", host.Screen().String())
	}
}

func TestMultiAttachViewportCropsWideHostOutputInsteadOfWrapping(t *testing.T) {
	h := newHarness(t)

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-crop",
		SessionName: "attach-camera-crop",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-crop"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-camera-crop",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.Cancel)

	host.Send("printf 'LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-END\\n'\n")
	if !screenContainsWithin(host, "RIGHT-END", 3*time.Second) {
		t.Fatalf("expected host to render full wide line before attach assertion")
	}
	if !screenContainsWithin(attach, "LEFT-1234567890", 3*time.Second) {
		t.Fatalf("expected attach viewport to show left edge of wide line")
	}
	if attach.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected attach viewport to crop wide line instead of wrapping right edge into view, got:\n%s", attach.Screen().String())
	}
}

func TestMultiAttachWithoutExplicitTermSizeMatchesControlViewportAcrossStartupResizeAndCommand(t *testing.T) {
	h := newHarness(t)

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-real-cli",
		SessionName: "attach-camera-real-cli",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-real-cli"})
	host.Send("ps aux\n")
	if !screenContainsWithin(host, "ps aux", 3*time.Second) {
		t.Fatalf("expected host to render ps output before attach startup")
	}

	requestControl := false
	control := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:      "attach-camera-real-cli",
		Cols:           80,
		Rows:           24,
		RequestControl: &requestControl,
	})
	t.Cleanup(control.Cancel)

	realCLI := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID:      "attach-camera-real-cli",
		Cols:           80,
		Rows:           24,
		RequestControl: &requestControl,
	})
	t.Cleanup(realCLI.session.Cancel)

	waitForTabLabels(t, control, []string{"attach-camera-real-cli"}, 5*time.Second)
	waitForTabLabels(t, realCLI.session, []string{"attach-camera-real-cli"}, 5*time.Second)

	assertBodyMatchesWithin(t, control, realCLI.session, "startup", 2*time.Second)

	control.Resize(60, 16)
	realCLI.resize(60, 16)

	assertBodyMatchesWithin(t, control, realCLI.session, "resize", 2*time.Second)

	host.Send("ps aux\n")
	if !screenContainsWithin(control, "ps aux", 3*time.Second) {
		t.Fatalf("expected control attach to show ps output after resize")
	}
	if !screenContainsWithin(realCLI.session, "ps aux", 3*time.Second) {
		t.Fatalf("expected real-cli attach to show ps output after resize")
	}

	assertBodyMatchesWithin(t, control, realCLI.session, "command", 2*time.Second)
}

type attachSessionWithResize struct {
	session *ptytest.PTYSession
	resize  func(cols, rows int)
}

func startMultiAttachWithoutExplicitTermSize(t *testing.T, h *ptytest.Harness, opts ptytest.MultiAttachOptions) attachSessionWithResize {
	t.Helper()
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}

	master, slave := ptytest.OpenPTY(t, opts.Cols, opts.Rows)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, opts.Cols, opts.Rows, h.Clock())
	resizeCh := make(chan struct{}, 1)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	token := opts.AccessToken
	if token == "" {
		token = h.AccessToken()
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = h.Endpoint()
	}
	authFile := opts.AuthFile
	if authFile == "" && opts.AccessToken == "" && opts.SessionSource == nil {
		authFile = h.AuthFile()
	}
	requestControl := true
	if opts.RequestControl != nil {
		requestControl = *opts.RequestControl
	}

	client := &attach.MultiClient{
		Endpoint:                    endpoint,
		AccessToken:                 token,
		SessionID:                   opts.SessionID,
		RequestControl:              requestControl,
		DisableDesktopNotifications: true,
		Stdin:                       slave,
		Stdout:                      slave,
		Stderr:                      io.Discard,
		ResizeEvents:                resizeCh,
		DisableSignalResize:         true,
		AuthFile:                    authFile,
		AllowOfflineToggle:          opts.AllowOfflineToggle,
		SessionSource:               opts.SessionSource,
		SocketResolver:              opts.SocketResolver,
		SessionEvents:               opts.SessionEvents,
		Clock:                       h.Clock(),
		OnView:                      opts.OnView,
		OnReconnect:                 opts.OnReconnect,
		OnViewClosed:                opts.OnViewClosed,
		OnActive:                    opts.OnActive,
		BackoffPolicy:               opts.BackoffPolicy,
		InactiveTTL:                 opts.InactiveTTL,
		RefreshInterval:             opts.RefreshInterval,
	}

	go func() {
		sess.SetRunErr(client.Run(sess.Context()))
	}()
	return attachSessionWithResize{
		session: sess,
		resize: func(cols, rows int) {
			sess.Resize(cols, rows)
			select {
			case resizeCh <- struct{}{}:
			default:
			}
		},
	}
}

func assertBodyMatchesWithin(t *testing.T, want, got *ptytest.PTYSession, phase string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(want.Clock()).Add(timeout)
	for {
		wantScreen := want.Screen()
		gotScreen := got.Screen()
		if attachBody(wantScreen) == attachBody(gotScreen) {
			return
		}
		if !ptytest.Now(want.Clock()).Before(deadline) {
			t.Fatalf("%s body mismatch\ncontrol:\n%s\n\nreal-cli:\n%s", phase, wantScreen.String(), gotScreen.String())
		}
		ptytest.Advance(want.Clock(), 50*time.Millisecond)
	}
}

func attachBody(screen ptytest.Screen) string {
	if screen.Rows <= 1 {
		return ""
	}
	lines := make([]string, 0, screen.Rows-1)
	for row := 1; row < screen.Rows; row++ {
		lines = append(lines, screen.Row(row))
	}
	return strings.Join(lines, "\n")
}
