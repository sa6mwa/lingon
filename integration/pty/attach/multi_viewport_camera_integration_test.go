//go:build integration
// +build integration

package integrationptyattach_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

var (
	attachCLIBuildOnce sync.Once
	attachCLIBuildPath string
	attachCLIBuildErr  error
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
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-camera-crop",
		SessionName: "attach-camera-crop",
		Shell:       writeAttachPromptShell(t),
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-camera-crop"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before crop assertion")
	}

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
	emitShellAgnosticLargeOutput(t, host, "ROW-080 012345678901234567890123456789", 3*time.Second)

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

	host.Send("i=1; while [ $i -le 40 ]; do printf 'CMD-%03d 012345678901234567890123456789\\n' \"$i\"; i=$((i+1)); done\n")
	if !screenContainsWithin(control, "CMD-040 012345678901234567890123456789", 3*time.Second) {
		t.Fatalf("expected control attach to show deterministic command output after resize")
	}
	if !screenContainsWithin(realCLI.session, "CMD-040 012345678901234567890123456789", 3*time.Second) {
		t.Fatalf("expected real-cli attach to show deterministic command output after resize")
	}

	assertBodyMatchesWithin(t, control, realCLI.session, "command", 2*time.Second)
}

func TestMultiAttachRealCLIControlDoesNotSendResizeAndEchoesPromptly(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(rec))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-lag-bash.sh"
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

func TestMultiAttachRealCLIControlBuffersStartupInputUntilViewReady(t *testing.T) {
	h := newHarness(t)

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-startup-input-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write attach startup-input bash wrapper: %v", err)
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-startup-input",
		SessionName: "attach-startup-input",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-startup-input"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before attach startup, got:\n%s", host.Screen().String())
	}

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-startup-input",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	attach.session.Send("echo ATTACH_STARTUP_READY\r")

	if !screenContainsWithin(host, "ATTACH_STARTUP_READY", 3*time.Second) {
		t.Fatalf("expected input typed during multi-attach startup to reach host, got host:\n%s\nattach:\n%s", host.Screen().String(), attach.session.Screen().String())
	}
	if !screenContainsWithin(attach.session, "ATTACH_STARTUP_READY", 3*time.Second) {
		t.Fatalf("expected startup input output to render in attach once ready, got:\n%s", attach.session.Screen().String())
	}
}

func TestMultiAttachRealCLIControlRepeatedSingleByteInputStaysResponsive(t *testing.T) {
	h := newHarness(t)

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-repeated-byte-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write attach repeated-byte bash wrapper: %v", err)
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-repeated-byte",
		SessionName: "attach-repeated-byte",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-repeated-byte"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before attach startup, got:\n%s", host.Screen().String())
	}

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-repeated-byte",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	attach.session.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.String(), "PROMPT>") {
			return fmt.Errorf("prompt not visible yet")
		}
		return nil
	})
	waitForClientCount(t, h, "attach-repeated-byte", 1, 3*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach.session, 5*time.Second)

	attach.resize(50, 14)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	prefix := ""
	for _, burst := range []string{"abcde", "fghij", "klmno", "pqrst"} {
		prefix += burst
		attach.session.Send(burst)
		if !screenContainsWithin(host, "PROMPT> "+prefix, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prefix %q within 350ms, got:\n%s", prefix, host.Screen().String())
		}
		if !screenContainsWithin(attach.session, "PROMPT> "+prefix, 700*time.Millisecond) {
			t.Fatalf("expected attach echo for prefix %q within 700ms, got:\n%s", prefix, attach.session.Screen().String())
		}
	}
}

func TestMultiAttachRealCLIControlRepeatedSingleByteInputStaysResponsiveRealClock(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-repeated-byte-realclock-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write attach repeated-byte realclock bash wrapper: %v", err)
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-repeated-byte-realclock",
		SessionName: "attach-repeated-byte-realclock",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-repeated-byte-realclock"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before attach startup, got:\n%s", host.Screen().String())
	}

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-repeated-byte-realclock",
		Cols:      60,
		Rows:      16,
	})
	t.Cleanup(attach.session.Cancel)

	waitForClientCount(t, h, "attach-repeated-byte-realclock", 1, 15*time.Second)
	ensureAttachPromptVisibleRealTime(t, host, attach.session, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach.session, 5*time.Second)
	attach.resize(50, 14)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	prefix := ""
	for _, burst := range []string{"abcde", "fghij", "klmno", "pqrst"} {
		prefix += burst
		start := time.Now()
		attach.session.Send(burst)
		if !screenContainsWithin(host, "PROMPT> "+prefix, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prefix %q within 350ms, got:\n%s", prefix, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithin(attach.session, "PROMPT> "+prefix, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prefix %q within 1200ms, got:\n%s", prefix, attach.session.Screen().String())
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("attach echo for prefix %q took %v after host echoed in %v\nattach:\n%s", prefix, attachElapsed, hostElapsed, attach.session.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLIRepeatedInputStaysResponsiveRealClock(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-external-cli-bash.sh"
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write attach external-cli bash wrapper: %v", err)
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli",
		SessionName: "attach-external-cli",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before external attach startup, got:\n%s", host.Screen().String())
	}

	attach := startLingonAttachCLI(t, h, "attach-external-cli", 60, 16)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli", 1, 15*time.Second)
	ensureAttachPromptVisibleRealTime(t, host, attach, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach, 5*time.Second)

	attach.Resize(50, 14)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	prefix := ""
	for _, burst := range []string{"abcde", "fghij", "klmno", "pqrst"} {
		prefix += burst
		start := time.Now()
		attach.Send(burst)
		if !screenContainsWithin(host, "PROMPT> "+prefix, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prefix %q within 350ms, got:\n%s", prefix, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, "PROMPT> "+prefix, 1200*time.Millisecond) {
			t.Fatalf("expected external attach echo for prefix %q within 1200ms after host echoed in %v, got:\n%s", prefix, hostElapsed, attach.Screen().String())
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("external attach echo for prefix %q took %v after host echoed in %v\nattach:\n%s", prefix, attachElapsed, hostElapsed, attach.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLIRepeatedSingleByteCommandsDoNotAccumulateLatencyRealClock(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-latency",
		SessionName: "attach-external-cli-latency",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli-latency"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)

	attach := startLingonAttachCLI(t, h, "attach-external-cli-latency", 60, 16)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli-latency", 1, 15*time.Second)
	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, host, attach, 1, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach, 5*time.Second)

	attach.Resize(26, 8)
	ptytest.Advance(h.Clock(), 300*time.Millisecond)

	for prompt := startPrompt; prompt < startPrompt+20; prompt++ {
		line := fmt.Sprintf("PROMPT-%03d> :", prompt)
		start := time.Now()
		attach.Send(":")
		if !screenContainsWithin(host, line, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prompt %03d within 350ms, got:\n%s", prompt, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, line, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prompt %03d within 1200ms after host echoed in %v, got:\n%s", prompt, hostElapsed, attach.Screen().String())
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("attach echo for prompt %03d took %v after host echoed in %v\nattach:\n%s", prompt, attachElapsed, hostElapsed, attach.Screen().String())
		}

		start = time.Now()
		attach.Send("\r")
		waitForPromptNumberWithin(t, host, prompt+1, 1200*time.Millisecond)
		hostElapsed = time.Since(start)
		waitForPromptNumberWithinRealTime(t, attach, prompt+1, 2*time.Second)
		attachElapsed = time.Since(start)
		if attachElapsed > 2*time.Second {
			t.Fatalf("attach prompt advance to %03d took %v after host advanced in %v\nattach:\n%s", prompt+1, attachElapsed, hostElapsed, attach.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveWithBackgroundSessionOutput(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-latency-a",
		SessionName: "attach-external-cli-latency-a",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-latency-b",
		SessionName: "attach-external-cli-latency-b",
		Shell:       "/bin/bash",
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli-latency-a", "attach-external-cli-latency-b"})
	waitForPromptNumberWithin(t, hostA, 1, 3*time.Second)
	hostB.Send("i=0; while [ $i -lt 400 ]; do echo NOISE-$i; i=$((i+1)); sleep 0.01; done &\n")
	if !screenContainsWithin(hostB, "NOISE-5", 3*time.Second) {
		t.Fatalf("expected background session noise before attach startup, got:\n%s", hostB.Screen().String())
	}

	attach := startLingonAttachCLI(t, h, "attach-external-cli-latency-a", 26, 8)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli-latency-a", 1, 15*time.Second)
	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, hostA, attach, 1, 10*time.Second)

	for prompt := startPrompt; prompt < startPrompt+12; prompt++ {
		line := fmt.Sprintf("PROMPT-%03d> :", prompt)
		start := time.Now()
		attach.Send(":")
		if !screenContainsWithin(hostA, line, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prompt %03d within 350ms with background session noise, got:\n%s", prompt, hostA.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, line, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prompt %03d within 1200ms after host echoed in %v with background session noise, got:\n%s", prompt, hostElapsed, attach.Screen().String())
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("attach echo for prompt %03d took %v after host echoed in %v with background session noise\nattach:\n%s", prompt, attachElapsed, hostElapsed, attach.Screen().String())
		}

		start = time.Now()
		attach.Send("\r")
		waitForPromptNumberWithin(t, hostA, prompt+1, 1200*time.Millisecond)
		hostElapsed = time.Since(start)
		waitForPromptNumberWithinRealTime(t, attach, prompt+1, 2*time.Second)
		attachElapsed = time.Since(start)
		if attachElapsed > 2*time.Second {
			t.Fatalf("attach prompt advance to %03d took %v after host advanced in %v with background session noise\nattach:\n%s", prompt+1, attachElapsed, hostElapsed, attach.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveAfterLargeHostOutput(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	var (
		rawPTYMu sync.Mutex
		rawPTY   bytes.Buffer
	)

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-large-output",
		SessionName: "attach-external-cli-large-output",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
		OnPTYRead: func(data []byte) {
			rawPTYMu.Lock()
			_, _ = rawPTY.Write(data)
			rawPTYMu.Unlock()
		},
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli-large-output"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)

	emitLargeOutputAndWaitForPromptNumber(t, host, 2, 3*time.Second)

	attach := startLingonAttachCLI(t, h, "attach-external-cli-large-output", 26, 8)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli-large-output", 1, 15*time.Second)
	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, host, attach, 2, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach, 5*time.Second)

	for prompt := startPrompt; prompt < startPrompt+13; prompt++ {
		line := fmt.Sprintf("PROMPT-%03d> :", prompt)
		start := time.Now()
		attach.Send(":")
		if !screenContainsWithin(host, line, 350*time.Millisecond) {
			rawSeenAt, rawSeen := waitForRawPTYContainsAfter(&rawPTYMu, &rawPTY, line, start, 5*time.Second)
			if rawSeen {
				t.Fatalf("expected host echo for prompt %03d within 350ms after large output; raw child PTY saw it after %v but host screen lagged\nhost:\n%s", prompt, rawSeenAt.Sub(start), host.Screen().String())
			}
			t.Fatalf("expected host echo for prompt %03d within 350ms after large output, and raw child PTY also did not show it within 5s\nhost:\n%s", prompt, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, line, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prompt %03d within 1200ms after host echoed in %v after large output, got:\n%s", prompt, hostElapsed, attach.Screen().String())
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("attach echo for prompt %03d took %v after host echoed in %v after large output\nattach:\n%s", prompt, attachElapsed, hostElapsed, attach.Screen().String())
		}

		start = time.Now()
		attach.Send("\r")
		waitForPromptNumberWithin(t, host, prompt+1, 1200*time.Millisecond)
		hostElapsed = time.Since(start)
		waitForPromptNumberWithinRealTime(t, attach, prompt+1, 2*time.Second)
		attachElapsed = time.Since(start)
		if attachElapsed > 2*time.Second {
			t.Fatalf("attach prompt advance to %03d took %v after host advanced in %v after large output\nattach:\n%s", prompt+1, attachElapsed, hostElapsed, attach.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveAfterLargeHostOutputAndResizeChurn(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithClock(clock.New()), ptytest.WithWSRecorder(rec))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-large-output-resize",
		SessionName: "attach-external-cli-large-output-resize",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli-large-output-resize"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)

	emitLargeOutputAndWaitForPromptNumber(t, host, 2, 3*time.Second)

	attach := startLingonAttachCLI(t, h, "attach-external-cli-large-output-resize", 26, 8)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli-large-output-resize", 1, 15*time.Second)
	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, host, attach, 2, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach, 5*time.Second)

	sizes := [][2]int{{26, 8}, {40, 12}, {18, 6}, {32, 10}}
	for prompt := startPrompt; prompt < startPrompt+9; prompt++ {
		size := sizes[(prompt-startPrompt)%len(sizes)]
		attach.Resize(size[0], size[1])
		time.Sleep(150 * time.Millisecond)

		line := fmt.Sprintf("PROMPT-%03d> :", prompt)
		start := time.Now()
		attach.Send(":")
		if !screenContainsWithin(host, line, 350*time.Millisecond) {
			t.Fatalf("expected host echo for prompt %03d within 350ms after large output + resize churn, got:\n%s", prompt, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, line, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prompt %03d within 1200ms after host echoed in %v after large output + resize churn, got:\n%s\nframes=%s", prompt, hostElapsed, attach.Screen().String(), recentInputFrameTrace(rec, "attach-external-cli-large-output-resize"))
		}
		attachElapsed := time.Since(start)
		if attachElapsed > 1200*time.Millisecond {
			t.Fatalf("attach echo for prompt %03d took %v after host echoed in %v after large output + resize churn\nattach:\n%s", prompt, attachElapsed, hostElapsed, attach.Screen().String())
		}

		start = time.Now()
		attach.Send("\r")
		waitForPromptNumberWithin(t, host, prompt+1, 1200*time.Millisecond)
		hostElapsed = time.Since(start)
		waitForPromptNumberWithinRealTime(t, attach, prompt+1, 2*time.Second)
		attachElapsed = time.Since(start)
		if attachElapsed > 2*time.Second {
			t.Fatalf("attach prompt advance to %03d took %v after host advanced in %v after large output + resize churn\nattach:\n%s", prompt+1, attachElapsed, hostElapsed, attach.Screen().String())
		}
	}
}

func TestMultiAttachExternalCLICommandExecutionStaysResponsiveAfterLargeHostOutput(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-external-cli-command-lag",
		SessionName: "attach-external-cli-command-lag",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-external-cli-command-lag"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)

	emitLargeOutputAndWaitForPromptNumber(t, host, 2, 3*time.Second)

	attach := startLingonAttachCLI(t, h, "attach-external-cli-command-lag", 26, 8)
	t.Cleanup(attach.Cancel)

	waitForClientCount(t, h, "attach-external-cli-command-lag", 1, 15*time.Second)
	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, host, attach, 2, 10*time.Second)
	clearAttachConnectionBanner(t, h.Clock(), attach, 5*time.Second)

	for prompt := startPrompt; prompt < startPrompt+7; prompt++ {
		token := fmt.Sprintf("ATTACH-%03d", prompt)
		cmd := "echo " + token

		start := time.Now()
		attach.Send(cmd)
		if !screenContainsWithin(host, "PROMPT-"+fmt.Sprintf("%03d", prompt)+"> "+cmd, 350*time.Millisecond) {
			t.Fatalf("expected host command echo for prompt %03d within 350ms after large output, got:\n%s", prompt, host.Screen().String())
		}
		hostEchoElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, cmd, 1200*time.Millisecond) {
			t.Fatalf("expected attach command echo %q within 1200ms after host echoed in %v, got:\n%s", cmd, hostEchoElapsed, attach.Screen().String())
		}

		start = time.Now()
		attach.Send("\r")
		if !screenContainsWithin(host, token, 1200*time.Millisecond) {
			t.Fatalf("expected host command output %q within 1200ms after large output, got:\n%s", token, host.Screen().String())
		}
		hostExecElapsed := time.Since(start)
		if !screenContainsWithinRealTime(attach, token, 2*time.Second) {
			t.Fatalf("expected attach command output %q within 2s after host showed it in %v, got:\n%s", token, hostExecElapsed, attach.Screen().String())
		}
		waitForPromptNumberWithin(t, host, prompt+1, 1200*time.Millisecond)
		waitForPromptNumberWithinRealTime(t, attach, prompt+1, 2*time.Second)
	}
}

func TestMultiAttachRealCLIControlRepeatedSingleByteCommandsStayResponsiveAfterLargeHostOutput(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithClock(clock.New()), ptytest.WithWSRecorder(rec))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-realcli-large-output",
		SessionName: "attach-realcli-large-output",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-realcli-large-output"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)

	emitLargeOutputAndWaitForPromptNumber(t, host, 2, 3*time.Second)

	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "attach-realcli-large-output",
		Cols:      26,
		Rows:      8,
	})
	t.Cleanup(attach.session.Cancel)

	waitForPromptNumberWithin(t, attach.session, 2, 5*time.Second)
	waitForClientCount(t, h, "attach-realcli-large-output", 1, 5*time.Second)
	ptytest.Advance(h.Clock(), 4*time.Second)

	for prompt := 2; prompt <= 14; prompt++ {
		line := fmt.Sprintf("PROMPT-%03d> :", prompt)
		start := time.Now()
		attach.session.Send(":")
		if inputAt, ok := waitForClientInputFrameSince(rec, "attach-realcli-large-output", start, 1200*time.Millisecond); !ok {
			t.Fatalf("expected client input frame for prompt %03d within 1200ms after send; frames=%s", prompt, recentInputFrameTrace(rec, "attach-realcli-large-output"))
		} else if delay := inputAt.Sub(start); delay > 350*time.Millisecond {
			t.Fatalf("client input frame for prompt %03d took %v to reach relay\nframes=%s", prompt, delay, recentInputFrameTrace(rec, "attach-realcli-large-output"))
		}
		if hostInputAt, ok := waitForHostInputFrameSince(rec, "attach-realcli-large-output", start, 1200*time.Millisecond); !ok {
			t.Fatalf("expected host input frame for prompt %03d within 1200ms after send; frames=%s", prompt, recentInputFrameTrace(rec, "attach-realcli-large-output"))
		} else if delay := hostInputAt.Sub(start); delay > 350*time.Millisecond {
			t.Fatalf("host input frame for prompt %03d took %v to reach host\nframes=%s", prompt, delay, recentInputFrameTrace(rec, "attach-realcli-large-output"))
		}
		if !screenContainsWithin(host, line, 350*time.Millisecond) {
			if eventualAt, ok := waitForHostEchoAfter(host, line, start, 5*time.Second); ok {
				t.Fatalf("host echo for prompt %03d took %v after large output\nhost:\n%s", prompt, eventualAt.Sub(start), host.Screen().String())
			}
			t.Fatalf("expected host echo for prompt %03d within 350ms after large output, and it never arrived within 5s\nhost:\n%s", prompt, host.Screen().String())
		}
		hostElapsed := time.Since(start)
		if !screenContainsWithin(attach.session, line, 1200*time.Millisecond) {
			t.Fatalf("expected attach echo for prompt %03d within 1200ms after host echoed in %v after large output, got:\n%s", prompt, hostElapsed, attach.session.Screen().String())
		}

		attach.session.Send("\r")
		waitForPromptNumberWithin(t, host, prompt+1, 1200*time.Millisecond)
		waitForPromptNumberWithin(t, attach.session, prompt+1, 2*time.Second)
	}
}

func TestRelayHostDoesNotKeepPublishingSnapshotsAfterLargeOutputSettles(t *testing.T) {
	rec := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithClock(clock.New()), ptytest.WithWSRecorder(rec))

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-large-output-idle",
		SessionName: "host-large-output-idle",
		Shell:       countingPromptBashForAttach(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-large-output-idle"})
	waitForPromptNumberWithin(t, host, 1, 3*time.Second)
	emitLargeOutputAndWaitForPromptNumber(t, host, 2, 3*time.Second)

	before := countHostSnapshotFrames(rec, "host-large-output-idle")
	ptytest.Advance(h.Clock(), 500*time.Millisecond)
	after := countHostSnapshotFrames(rec, "host-large-output-idle")
	if delta := after - before; delta > 5 {
		t.Fatalf("expected settled host to stop snapshot churn after large output; saw %d new snapshots in 500ms", delta)
	}
}

func TestMultiAttachRealCLIControlWithMultipleSessionsKeepsViewportStable(t *testing.T) {
	h := newHarness(t)

	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := testutil.TempDir(t) + "/attach-multi-bash.sh"
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
	emitShellAgnosticLargeOutput(t, hostA, "ROW-080 012345678901234567890123456789", 3*time.Second)

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
	shell := testutil.TempDir(t) + "/attach-signal-bash.sh"
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
	emitShellAgnosticLargeOutput(t, hostA, "ROW-080 012345678901234567890123456789", 3*time.Second)

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
	if !screenContainsWithin(control, "ATTACH_SIGNAL", 800*time.Millisecond) {
		t.Fatalf("expected explicit attach to echo signal command promptly")
	}
	if !screenContainsWithin(signalCLI.session, "ATTACH_SIGNAL", 800*time.Millisecond) {
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

	startPrompt := ensureAttachPromptNumberVisibleRealTime(t, hostA, attach.session, 1, 10*time.Second)
	finalPrompt := startPrompt + 24

	attach.resize(32, 8)
	attach.session.SendBytes([]byte(strings.Repeat("\n", 24)))

	eventuallyWithClockAttach(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		hostNums := promptNumbersFromScreen(hostA.Screen().String())
		if len(hostNums) == 0 || hostNums[len(hostNums)-1] != finalPrompt {
			return fmt.Errorf("expected host to advance to prompt %d, got %v\npty=%q\nhost:\n%s", finalPrompt, hostNums, ptyOut.String(), hostA.Screen().String())
		}
		attachNums := promptNumbersFromScreen(attach.session.Screen().String())
		if len(attachNums) == 0 || attachNums[len(attachNums)-1] != finalPrompt {
			return fmt.Errorf("expected multi-attach real-cli to advance to prompt %d, got %v\npty=%q\nattach:\n%s", finalPrompt, attachNums, ptyOut.String(), attach.session.Screen().String())
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
	attach.session.Send("i=1; while [ $i -le 40 ]; do printf 'CROP-%03d 012345678901234567890123456789\\n' \"$i\"; i=$((i+1)); done\r")
	if !screenContainsWithin(hostA, "CROP-040 012345678901234567890123456789", 3*time.Second) {
		t.Fatalf("expected host to receive deterministic tall output after attach resize, got:\n%s", hostA.Screen().String())
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
			ptytest.Advance(h.Clock(), 50*time.Millisecond)
		},
	}
}

func startLingonAttachCLI(t *testing.T, h *ptytest.Harness, sessionID string, cols, rows int) *ptytest.PTYSession {
	t.Helper()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	bin := buildLingonAttachBinary(t)
	master, slave := ptytest.OpenPTY(t, cols, rows)
	resizeFD, err := syscall.Dup(int(slave.Fd()))
	if err != nil {
		t.Fatalf("dup attach slave pty: %v", err)
	}
	resizeSlave := os.NewFile(uintptr(resizeFD), slave.Name()+"-resize")
	sess := ptytest.NewPTYSessionWithClock(t, master, resizeSlave, cols, rows, h.Clock())
	t.Cleanup(func() {
		_ = master.Close()
		_ = resizeSlave.Close()
	})

	cmd := exec.Command(
		bin,
		"attach",
		sessionID,
		"--endpoint", h.Endpoint(),
		"--access-token", h.AccessToken(),
		"--auth-file", h.AuthFile(),
		"--insecure",
		"--request-control",
		"--disable-desktop-notifications",
	)
	homeDir := testutil.TempDir(t)
	cmd.Env = testAttachCLIEnv(homeDir)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lingon attach cli: %v", err)
	}
	_ = slave.Close()
	go func() {
		<-sess.Context().Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	go func() {
		err := cmd.Wait()
		if sess.Context().Err() != nil && (err == nil || errors.Is(err, os.ErrProcessDone) || strings.Contains(err.Error(), "signal: killed")) {
			err = nil
		}
		sess.SetRunErr(err)
	}()
	return sess
}

func testAttachCLIEnv(homeDir string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "XDG_CONFIG_HOME=") ||
			strings.HasPrefix(entry, "XDG_CACHE_HOME=") ||
			strings.HasPrefix(entry, "XDG_STATE_HOME=") ||
			strings.HasPrefix(entry, "XDG_DATA_HOME=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"XDG_CONFIG_HOME="+filepath.Join(homeDir, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(homeDir, ".cache"),
		"XDG_STATE_HOME="+filepath.Join(homeDir, ".state"),
		"XDG_DATA_HOME="+filepath.Join(homeDir, ".local", "share"),
	)
	return env
}

func buildLingonAttachBinary(t *testing.T) string {
	t.Helper()
	attachCLIBuildOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			attachCLIBuildErr = fmt.Errorf("resolve caller path")
			return
		}
		repoRoot, err := findRepoRoot(filepath.Dir(file))
		if err != nil {
			attachCLIBuildErr = err
			return
		}
		if attachIntegrationTempRoot == "" {
			attachCLIBuildErr = fmt.Errorf("attach integration temp root not initialized")
			return
		}
		dir := filepath.Join(attachIntegrationTempRoot, "lingon-attach-repro")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			attachCLIBuildErr = fmt.Errorf("create lingon attach build dir: %w", err)
			return
		}
		out := filepath.Join(dir, "lingon-attach-repro")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/lingon")
		cmd.Dir = repoRoot
		if output, err := cmd.CombinedOutput(); err != nil {
			attachCLIBuildErr = fmt.Errorf("go build lingon: %w\n%s", err, string(output))
			return
		}
		attachCLIBuildPath = out
	})
	if attachCLIBuildErr != nil {
		t.Fatalf("build lingon attach binary: %v", attachCLIBuildErr)
	}
	return attachCLIBuildPath
}

func findRepoRoot(start string) (string, error) {
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("find repo root from %s: go.mod not found", start)
		}
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

func waitForPromptNumberWithinRealTime(t *testing.T, sess *ptytest.PTYSession, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		nums := promptNumbersFromScreen(sess.Screen().String())
		if len(nums) > 0 && nums[len(nums)-1] == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	nums := promptNumbersFromScreen(sess.Screen().String())
	if len(nums) == 0 || nums[len(nums)-1] != want {
		t.Fatalf("waiting for prompt %d, got %v", want, nums)
	}
}

func screenContainsWithinRealTime(sess *ptytest.PTYSession, token string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sess.Screen().Contains(token) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sess.Screen().Contains(token)
}

func ensureAttachPromptVisibleRealTime(t *testing.T, host, attach *ptytest.PTYSession, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if attach.Screen().Contains("PROMPT>") {
			return
		}
		attach.Send("\r")
		if !screenContainsWithin(host, "PROMPT>", 1200*time.Millisecond) {
			continue
		}
		if screenContainsWithinRealTime(attach, "PROMPT>", 2*time.Second) {
			return
		}
	}
	t.Fatalf("expected attach prompt to become visible within %v, got:\nhost:\n%s\nattach:\n%s", timeout, host.Screen().String(), attach.Screen().String())
}

func ensureAttachPromptNumberVisibleRealTime(t *testing.T, host, attach *ptytest.PTYSession, startPrompt int, timeout time.Duration) int {
	t.Helper()
	current := startPrompt
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		line := fmt.Sprintf("PROMPT-%03d>", current)
		if screenContainsWithinRealTime(attach, line, 200*time.Millisecond) {
			return current
		}
		attach.Send("\r")
		current++
		waitForPromptNumberWithin(t, host, current, 1200*time.Millisecond)
		if screenContainsWithinRealTime(attach, fmt.Sprintf("PROMPT-%03d>", current), 2*time.Second) {
			return current
		}
	}
	t.Fatalf("expected attach numbered prompt to become visible within %v, got host:\n%s\nattach:\n%s", timeout, host.Screen().String(), attach.Screen().String())
	return current
}

func clearAttachConnectionBanner(t *testing.T, clk clock.Clock, attach *ptytest.PTYSession, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for !ptytest.Now(clk).After(deadline) {
		if !hasConnectionStatusBanner(attach.Screen().Row(0)) {
			return
		}
		ptytest.Advance(clk, 250*time.Millisecond)
	}
	t.Fatalf("expected attach connection banner to clear within %v, got:\n%s", timeout, attach.Screen().String())
}

func emitLargeOutputAndWaitForPromptNumber(t *testing.T, host *ptytest.PTYSession, nextPrompt int, timeout time.Duration) {
	t.Helper()
	host.Send("for i in $(seq 1 120); do printf 'ROW-%03d 0123456789012345678901234567890123456789\\n' \"$i\"; done\r")
	if !screenContainsWithin(host, "ROW-120 0123456789012345678901234567890123456789", timeout) {
		t.Fatalf("expected deterministic large output before prompt %03d, got:\n%s", nextPrompt, host.Screen().String())
	}
	waitForPromptNumberWithin(t, host, nextPrompt, timeout)
}

func emitShellAgnosticLargeOutput(t *testing.T, host *ptytest.PTYSession, lastLine string, timeout time.Duration) {
	t.Helper()
	host.Send("i=1; while [ $i -le 80 ]; do printf 'ROW-%03d 012345678901234567890123456789\\n' \"$i\"; i=$((i+1)); done\n")
	if !screenContainsWithin(host, lastLine, timeout) {
		t.Fatalf("expected deterministic shell-agnostic output ending in %q, got:\n%s", lastLine, host.Screen().String())
	}
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

func waitForClientInputFrameSince(rec *ptytest.WSRecorder, sessionID string, start time.Time, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, frame := range rec.Frames() {
			if frame.Role != "client" || frame.Direction != ptytest.DirClientToServer || frame.Payload != "input" {
				continue
			}
			if frame.SessionID != "" && frame.SessionID != sessionID {
				continue
			}
			if !frame.Time.Before(start) {
				return frame.Time, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, frame := range rec.Frames() {
		if frame.Role != "client" || frame.Direction != ptytest.DirClientToServer || frame.Payload != "input" {
			continue
		}
		if frame.SessionID != "" && frame.SessionID != sessionID {
			continue
		}
		if !frame.Time.Before(start) {
			return frame.Time, true
		}
	}
	return time.Time{}, false
}

func waitForHostInputFrameSince(rec *ptytest.WSRecorder, sessionID string, start time.Time, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, frame := range rec.Frames() {
			if frame.Role != "host" || frame.Direction != ptytest.DirServerToClient || frame.SessionID != sessionID || frame.Payload != "input" {
				continue
			}
			if !frame.Time.Before(start) {
				return frame.Time, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, frame := range rec.Frames() {
		if frame.Role != "host" || frame.Direction != ptytest.DirServerToClient || frame.SessionID != sessionID || frame.Payload != "input" {
			continue
		}
		if !frame.Time.Before(start) {
			return frame.Time, true
		}
	}
	return time.Time{}, false
}

func recentInputFrameTrace(rec *ptytest.WSRecorder, sessionID string) string {
	frames := rec.Frames()
	if len(frames) == 0 {
		return "<none>"
	}
	var parts []string
	for i := len(frames) - 1; i >= 0 && len(parts) < 12; i-- {
		frame := frames[i]
		if frame.SessionID != sessionID {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s %s seq=%d at=%s", frame.Role, frame.Direction, frame.Payload, frame.Seq, frame.Time.Format("15:04:05.000")))
	}
	if len(parts) == 0 {
		return "<none>"
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " | ")
}

func countHostSnapshotFrames(rec *ptytest.WSRecorder, sessionID string) int {
	count := 0
	for _, frame := range rec.Frames() {
		if frame.Role == "host" && frame.Direction == ptytest.DirClientToServer && frame.SessionID == sessionID && frame.Payload == "snapshot" {
			count++
		}
	}
	return count
}

func waitForHostEchoAfter(host *ptytest.PTYSession, line string, start time.Time, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if host.Screen().Contains(line) {
			return time.Now(), true
		}
		ptytest.Advance(host.Clock(), 25*time.Millisecond)
	}
	if host.Screen().Contains(line) {
		return time.Now(), true
	}
	return time.Time{}, false
}

func waitForRawPTYContainsAfter(mu *sync.Mutex, buf *bytes.Buffer, needle string, start time.Time, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := strings.Contains(buf.String(), needle)
		mu.Unlock()
		if seen {
			return time.Now(), true
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	seen := strings.Contains(buf.String(), needle)
	mu.Unlock()
	if seen {
		return time.Now(), true
	}
	return time.Time{}, false
}

func countingPromptBashForAttach(t *testing.T) string {
	t.Helper()
	dir := testutil.TempDir(t)
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
