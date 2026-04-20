package attach_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
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

func TestMultiAttachRealCLIControlDoesNotSendResizeAndEchoesPromptly(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(rec))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := t.TempDir() + "/attach-lag-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write attach lag bash wrapper: %v", err)
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-lag",
		SessionName: "attach-camera-lag",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-lag"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before attach startup, got:\n%s", host.Screen().String())
	}

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-camera-lag",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	attach.session.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		snapshot := screen.String()
		if !strings.Contains(snapshot, "PROMPT>") {
			return fmt.Errorf("prompt not visible yet")
		}
		return nil
	})
	waitForClientCount(t, h, "attach-camera-lag", 1, 3*time.Second)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)
	assertNoClientResizeFrames(t, rec, "attach-camera-lag")

	attach.resize(50, 14)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)
	assertNoClientResizeFrames(t, rec, "attach-camera-lag")

	attach.session.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.String(), "PROMPT>") {
			return fmt.Errorf("prompt missing after resize:\n%s", screen.String())
		}
		return nil
	})

	start := time.Now()
	attach.session.Send("e")
	if !screenContainsWithin(attach.session, "PROMPT> e", 300*time.Millisecond) {
		t.Fatalf("expected echoed input to appear promptly after first byte, got:\n%s", attach.session.Screen().String())
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("expected first echoed byte within 300ms, took %v", elapsed)
	}

	attach.session.Send("cho ATTACH_LAG_OK\r")
	if !screenContainsWithin(attach.session, "ATTACH_LAG_OK", 700*time.Millisecond) {
		t.Fatalf("expected command output to appear promptly after Enter, got:\n%s", attach.session.Screen().String())
	}
	if !screenContainsWithin(attach.session, "PROMPT>", 700*time.Millisecond) {
		t.Fatalf("expected prompt redraw after command, got:\n%s", attach.session.Screen().String())
	}

	host.Send("stty size; echo SIZE_DONE\n")
	if !screenContainsWithin(host, "SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected host size probe after attach input")
	}
	if !screenContainsWithin(host, "30 120", 2*time.Second) {
		t.Fatalf("expected relay-backed host PTY to remain 120x30 after attach resize/input, got:\n%s", host.Screen().String())
	}
	assertNoClientResizeFrames(t, rec, "attach-camera-lag")
}

func TestMultiAttachRealCLIControlWithMultipleSessionsKeepsViewportStable(t *testing.T) {
	h := newHarness(t)

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := t.TempDir() + "/attach-multi-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write multi attach bash wrapper: %v", err)
	}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-multi-a",
		SessionName: "attach-multi-a",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-multi-b",
		SessionName: "attach-multi-b",
		Shell:       "/bin/sh",
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-multi-a", "attach-multi-b"})
	hostA.Send("ps aux\n")
	if !screenContainsWithin(hostA, "ps aux", 3*time.Second) {
		t.Fatalf("expected host A ps output before multi-attach startup, got:\n%s", hostA.Screen().String())
	}

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-multi-a",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	attach.session.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if !strings.Contains(row0, "attach-multi-a") {
			return fmt.Errorf("tab bar missing session labels; row=%q screen:\n%s", row0, screen.String())
		}
		body := attachBody(screen)
		if !strings.Contains(body, "PROMPT>") {
			return fmt.Errorf("expected prompt in attach body on startup:\n%s", screen.String())
		}
		cur := attach.session.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("cursor not below top row; row=%d col=%d screen:\n%s", cur.Row, cur.Col, screen.String())
		}
		return nil
	})

	attach.resize(40, 10)
	attach.session.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		body := attachBody(screen)
		if !strings.Contains(body, "PROMPT>") {
			return fmt.Errorf("expected prompt to remain visible after resize:\n%s", screen.String())
		}
		cur := attach.session.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("cursor not below top row after resize; row=%d col=%d screen:\n%s", cur.Row, cur.Col, screen.String())
		}
		return nil
	})

	start := time.Now()
	attach.session.Send("ps aux\r")
	if !screenContainsWithin(attach.session, "ps aux", 500*time.Millisecond) {
		t.Fatalf("expected echoed command promptly in multi-attach viewport, got:\n%s", attach.session.Screen().String())
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected echoed command within 500ms, took %v", elapsed)
	}
	attach.session.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		body := attachBody(screen)
		if !strings.Contains(body, "PROMPT>") {
			return fmt.Errorf("prompt missing after command:\n%s", screen.String())
		}
		cur := attach.session.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("cursor not below top row after command; row=%d col=%d screen:\n%s", cur.Row, cur.Col, screen.String())
		}
		return nil
	})
}

func TestMultiAttachSignalResizeWithMultipleSessionsMatchesExplicitViewport(t *testing.T) {
	h := newHarness(t)

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := t.TempDir() + "/attach-signal-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write signal bash wrapper: %v", err)
	}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-signal-a",
		SessionName: "attach-signal-a",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-signal-b",
		SessionName: "attach-signal-b",
		Shell:       "/bin/sh",
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-signal-a", "attach-signal-b"})
	hostA.Send("ps aux\n")
	if !screenContainsWithin(hostA, "ps aux", 3*time.Second) {
		t.Fatalf("expected host A ps output before attach startup, got:\n%s", hostA.Screen().String())
	}

	control := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-signal-a",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(control.Cancel)

	signalCLI := startMultiAttachWithSignalResize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-signal-a",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(signalCLI.session.Cancel)

	assertBodyMatchesWithin(t, control, signalCLI.session, "signal-startup", 3*time.Second)

	control.Resize(40, 10)
	signalCLI.resize(40, 10)
	assertBodyMatchesWithin(t, control, signalCLI.session, "signal-resize", 3*time.Second)

	control.Send("echo ATTACH_SIGNAL\r")
	signalCLI.session.Send("echo ATTACH_SIGNAL\r")
	if !screenContainsWithin(control, "ATTACH_SIGNAL", 500*time.Millisecond) {
		t.Fatalf("expected explicit attach to echo signal command promptly")
	}
	if !screenContainsWithin(signalCLI.session, "ATTACH_SIGNAL", 500*time.Millisecond) {
		t.Fatalf("expected signal-resize attach to echo command promptly")
	}
	assertBodyMatchesWithin(t, control, signalCLI.session, "signal-command", 2*time.Second)
}

func TestMultiAttachRealCLIControlBurstEnterKeepsConsecutiveBashPromptNumbers(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	var ptyOut bytes.Buffer
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "prompt-burst-multi-a",
		SessionName: "prompt-burst-multi-a",
		Shell:       countingPromptBashForAttach(t),
		Cols:        80,
		Rows:        16,
		OnPTYRead: func(data []byte) {
			_, _ = ptyOut.Write(data)
		},
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "prompt-burst-multi-b",
		SessionName: "prompt-burst-multi-b",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        16,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"prompt-burst-multi-a", "prompt-burst-multi-b"})
	waitForPromptNumberWithin(t, hostA, 1, 3*time.Second)

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "prompt-burst-multi-a",
		Cols:      40,
		Rows:      8,
	})
	t.Cleanup(attach.session.Cancel)

	waitForPromptNumberWithin(t, attach.session, 1, 3*time.Second)

	attach.resize(32, 8)
	attach.session.SendBytes([]byte(strings.Repeat("\n", 24)))

	eventuallyWithClockAttach(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		hostNums := promptNumbersFromScreen(hostA.Screen().String())
		if len(hostNums) == 0 || hostNums[len(hostNums)-1] != 25 {
			return fmt.Errorf("expected host to advance to prompt 25, got %v\npty=%q\nhost:\n%s", hostNums, ptyOut.String(), hostA.Screen().String())
		}
		attachNums := promptNumbersFromScreen(attach.session.Screen().String())
		if len(attachNums) == 0 || attachNums[len(attachNums)-1] != 25 {
			return fmt.Errorf("expected multi-attach real-cli to advance to prompt 25, got %v\npty=%q\nattach:\n%s", attachNums, ptyOut.String(), attach.session.Screen().String())
		}
		return nil
	})
}

func TestMultiAttachRealCLIControlPsAuxAfterResizeMatchesHostCrop(t *testing.T) {
	h := newHarness(t)

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-ps-a",
		SessionName: "attach-ps-a",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-ps-b",
		SessionName: "attach-ps-b",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-ps-a", "attach-ps-b"})

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-ps-a",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	attach.resize(40, 10)
	attach.session.Send("ps aux\r")
	if !screenContainsWithin(hostA, "ps aux", 3*time.Second) {
		t.Fatalf("expected host to receive ps aux after attach resize, got:\n%s", hostA.Screen().String())
	}
	assertMultiAttachBodyMatchesHostCropWithin(t, hostA, attach.session, 40, 10, 3*time.Second, "ps-aux")
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

func startMultiAttachWithSignalResize(t *testing.T, h *ptytest.Harness, opts ptytest.MultiAttachOptions) attachSessionWithResize {
	t.Helper()
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}

	master, slave := ptytest.OpenPTY(t, opts.Cols, opts.Rows)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, opts.Cols, opts.Rows, h.Clock())
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
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
			ptytest.Advance(h.Clock(), 50*time.Millisecond)
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

func assertNoClientResizeFrames(t *testing.T, rec *ptytest.WSRecorder, sessionID string) {
	t.Helper()
	for _, frame := range rec.Frames() {
		if frame.Role == "client" && frame.Direction == ptytest.DirClientToServer && frame.SessionID == sessionID && frame.Payload == "resize" {
			t.Fatalf("unexpected client resize frame for %s: %+v", sessionID, frame)
		}
	}
}

func assertMultiAttachBodyMatchesHostCropWithin(t *testing.T, host, attach *ptytest.PTYSession, cols, rows int, timeout time.Duration, phase string) {
	t.Helper()
	deadline := ptytest.Now(host.Clock()).Add(timeout)
	for {
		want := cropHostBody(host.Screen(), cols, rows)
		got := attachBody(attach.Screen())
		if got == want {
			return
		}
		if !ptytest.Now(host.Clock()).Before(deadline) {
			t.Fatalf("%s crop mismatch\nwant:\n%s\n\ngot:\n%s\n\nhost:\n%s\n\nattach:\n%s", phase, want, got, host.Screen().String(), attach.Screen().String())
		}
		ptytest.Advance(host.Clock(), 50*time.Millisecond)
	}
}

func cropHostBody(screen ptytest.Screen, cols, rows int) string {
	if rows <= 1 || cols <= 0 || screen.Rows <= 1 {
		return ""
	}
	bodyRows := make([]string, 0, screen.Rows-1)
	for row := 1; row < screen.Rows; row++ {
		line := screen.Row(row)
		runes := []rune(line)
		if len(runes) > cols {
			line = string(runes[:cols])
		}
		bodyRows = append(bodyRows, line)
	}
	wantRows := rows - 1
	if wantRows > len(bodyRows) {
		wantRows = len(bodyRows)
	}
	start := len(bodyRows) - wantRows
	if start < 0 {
		start = 0
	}
	return strings.Join(bodyRows[start:], "\n")
}

var attachPromptNumberRe = regexp.MustCompile(`PROMPT-([0-9]{3})>`)

func promptNumbersFromScreen(screen string) []int {
	matches := attachPromptNumberRe.FindAllStringSubmatch(screen, -1)
	out := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func waitForPromptNumberWithin(t *testing.T, sess *ptytest.PTYSession, want int, timeout time.Duration) {
	t.Helper()
	eventuallyWithClockAttach(t, sess.Clock(), timeout, 50*time.Millisecond, func() error {
		nums := promptNumbersFromScreen(sess.Screen().String())
		if len(nums) == 0 {
			return fmt.Errorf("waiting for numbered prompt")
		}
		if nums[len(nums)-1] != want {
			return fmt.Errorf("waiting for prompt %d, got %v", want, nums)
		}
		return nil
	})
}

func eventuallyWithClockAttach(t *testing.T, clk clock.Clock, timeout, step time.Duration, check func() error) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for !ptytest.Now(clk).After(deadline) {
		err := check()
		if err == nil {
			return
		}
		ptytest.Advance(clk, step)
	}
	err := check()
	if err == nil {
		return
	}
	t.Fatal(err)
}

func countingPromptBashForAttach(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "bashrc")
	wrapperPath := filepath.Join(dir, "bash-wrapper.sh")
	const rc = `
count=0
update_prompt() {
  count=$((count+1))
  printf -v PS1 'PROMPT-%03d> ' "$count"
}
PROMPT_COMMAND=update_prompt
set +o emacs
set +o vi
`
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatalf("write attach bashrc: %v", err)
	}
	wrapper := fmt.Sprintf("#!/usr/bin/env bash\nexec /bin/bash --noprofile --rcfile %q -i\n", rcPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write counting prompt bash: %v", err)
	}
	return wrapperPath
}
