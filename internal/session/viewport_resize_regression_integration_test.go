package session_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostSIGWINCHResizesLocalPTY(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-local-host",
		SessionName: "viewport-local-host",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.Send("echo LOCAL_READY\n")
	waitForRawContains(t, host, "LOCAL_READY", 2*time.Second, 50*time.Millisecond, "expected local shell readiness marker")

	host.Send("stty size; echo LOCAL_SIZE_DONE\n")
	if !screenContainsWithin(host, "LOCAL_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected initial local PTY size marker on screen")
	}
	if !screenContainsWithin(host, "40 120", 2*time.Second) {
		t.Fatalf("expected initial local PTY size on screen, got:\n%s", host.Screen().String())
	}

	host.Resize(80, 24)
	advanceTestClock(host.Clock(), 200*time.Millisecond)

	host.Send("stty size; echo LOCAL_SIZE_AFTER_DONE\n")
	if !screenContainsWithin(host, "LOCAL_SIZE_AFTER_DONE", 2*time.Second) {
		t.Fatalf("expected local resize marker on screen")
	}
	if !screenContainsWithin(host, "24 80", 2*time.Second) {
		t.Fatalf("expected SIGWINCH to resize local PTY to 80x24, got:\n%s", host.Screen().String())
	}
}

func TestRemoteTabActivationKeepsRemotePTYSize(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-remote-source",
		SessionName: "viewport-remote-source",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-remote-viewer",
		SessionName: "viewport-remote-viewer",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.Send("echo SOURCE_READY\n")
	waitForRawContains(t, hostA, "SOURCE_READY", 2*time.Second, 50*time.Millisecond, "expected source shell readiness marker")

	hostA.Send("stty size; echo SOURCE_SIZE_DONE\n")
	if !screenContainsWithin(hostA, "SOURCE_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected initial source PTY size marker on source host")
	}
	if !screenContainsWithin(hostA, "40 120", 2*time.Second) {
		t.Fatalf("expected initial source PTY size on source host, got:\n%s", hostA.Screen().String())
	}

	reachedRemote := false
	for i := 0; i < 4; i++ {
		token := "REMOTE_VIEW_ACTIVE"
		hostB.SendCtrlL()
		hostB.Send("n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		hostB.Send("echo " + token + "\n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		if screenContainsWithin(hostA, token, 1500*time.Millisecond) {
			reachedRemote = true
			break
		}
	}
	if !reachedRemote {
		t.Fatalf("timed out switching viewer host to remote tab")
	}

	hostB.Send("stty size; echo REMOTE_SIZE_DONE\n")
	if !screenContainsWithin(hostA, "REMOTE_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected remote size marker on source host")
	}
	if !screenContainsWithin(hostA, "40 120", 2*time.Second) {
		t.Fatalf("expected remote tab activation to preserve source PTY size, got:\n%s", hostA.Screen().String())
	}
}

func TestLocalTabActivationResizesLocalPTYToCurrentViewport(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-remote-stable-source",
		SessionName: "viewport-remote-stable-source",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-local-resize-return",
		SessionName: "viewport-local-resize-return",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	reachedRemote := false
	for i := 0; i < 4; i++ {
		token := "REMOTE_VIEW_READY"
		hostB.SendCtrlL()
		hostB.Send("n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		hostB.Send("echo " + token + "\n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		if screenContainsWithin(hostA, token, 1500*time.Millisecond) {
			reachedRemote = true
			break
		}
	}
	if !reachedRemote {
		t.Fatalf("timed out switching viewer host to remote tab")
	}

	hostB.Resize(100, 30)
	advanceTestClock(hostB.Clock(), 200*time.Millisecond)

	hostB.Send("stty size; echo REMOTE_VIEWER_SIZE_DONE\n")
	if !screenContainsWithin(hostA, "REMOTE_VIEWER_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected remote viewer size marker on source host")
	}
	if !screenContainsWithin(hostA, "40 120", 2*time.Second) {
		t.Fatalf("expected remote viewer resize to preserve source PTY size, got:\n%s", hostA.Screen().String())
	}

	hostB.SendCtrlL()
	hostB.Send("p")
	advanceTestClock(hostB.Clock(), 200*time.Millisecond)

	hostB.Send("stty size; echo LOCAL_BACK_SIZE_DONE\n")
	if !screenContainsWithin(hostB, "LOCAL_BACK_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected local return resize marker on viewer host")
	}
	if !screenContainsWithin(hostB, "30 100", 2*time.Second) {
		t.Fatalf("expected local tab to adopt current viewport size on return, got:\n%s", hostB.Screen().String())
	}
}

func TestAttachControlResizeDoesNotResizeNonHeadlessHostPTY(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-no-resize-host",
		SessionName: "attach-no-resize-host",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	host.Send("echo HOST_READY\n")
	waitForRawContains(t, host, "HOST_READY", 2*time.Second, 50*time.Millisecond, "expected host readiness marker")

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "attach-no-resize-host",
		RequestControl: true,
		Cols:           80,
		Rows:           24,
	})
	t.Cleanup(attach.Cancel)

	attach.Send("printf 'ATTACH_CONTROL_READY\\n'\n")
	if !screenContainsWithin(host, "ATTACH_CONTROL_READY", 3*time.Second) {
		t.Fatalf("expected attach input to reach host")
	}

	attach.Send("printf 'SIZE1='; stty size; echo ' SIZE1_DONE'\n")
	if !screenContainsWithin(host, "SIZE1_DONE", 2*time.Second) {
		t.Fatalf("expected initial attach size marker on host")
	}
	if !screenContainsWithin(host, "SIZE1=40 120", 2*time.Second) {
		t.Fatalf("expected attach control acquisition to preserve non-headless host PTY size, got:\n%s", host.Screen().String())
	}

	attach.Resize(100, 30)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	attach.Send("printf 'SIZE2='; stty size; echo ' SIZE2_DONE'\n")
	if !screenContainsWithin(host, "SIZE2_DONE", 2*time.Second) {
		t.Fatalf("expected post-resize attach size marker on host")
	}
	if !screenContainsWithin(host, "SIZE2=40 120", 2*time.Second) {
		t.Fatalf("expected attach resize to preserve non-headless host PTY size, got:\n%s", host.Screen().String())
	}
}

func TestHostResizePreservesWideContentAcrossShrinkAndExpand(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-content",
		SessionName: "viewport-preserve-wide-content",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-wide-content", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("printf 'LEFT-1234567890-MID-abcdefghij-RIGHT-END\\n'\n")
	waitForStableSeededHostOutput(t, host, "RIGHT-END", 3*time.Second)

	host.Resize(20, 12)
	advanceTestClock(host.Clock(), 200*time.Millisecond)

	if !host.Screen().Contains("LEFT-") {
		t.Fatalf("expected shrink to preserve visible left edge, got:\n%s", host.Screen().String())
	}
	if host.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected shrink to hide right edge in narrow viewport, got:\n%s", host.Screen().String())
	}

	host.Resize(60, 12)
	advanceTestClock(host.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-END") {
			return fmt.Errorf("expected restored wide content after enlarging viewport, got:\n%s", host.Screen().String())
		}
		return nil
	})
}

func TestHostResizePreservesWideScreenWithoutInput(t *testing.T) {
	shell := preservedWideScreenShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-screen",
		SessionName: "viewport-preserve-wide-screen",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-wide-screen", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-12") {
			return fmt.Errorf("expected initial wide screen content, got:\n%s", host.Screen().String())
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(20, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	if host.Screen().Contains("RIGHT-06") || host.Screen().Contains("RIGHT-12") {
		t.Fatalf("expected shrink to hide right edge on wide stationary screen, got:\n%s", host.Screen().String())
	}
	_ = host.DrainRaw()

	host.Resize(60, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "RIGHT-06") || !strings.Contains(screen, "RIGHT-12") {
			return fmt.Errorf("expected expand to restore wide stationary screen without new input, got:\n%s", screen)
		}
		return nil
	})
	waitForRawContains(
		t,
		host,
		"RIGHT-12",
		2*time.Second,
		50*time.Millisecond,
		"expected Lingon to emit restored wide content after expand without relying on terminal-side preservation",
	)
}

func TestHostResizePreservesWideScreenWithBottomCursorWithoutInput(t *testing.T) {
	shell := preservedWideScreenBottomCursorShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-screen-bottom-cursor",
		SessionName: "viewport-preserve-wide-screen-bottom-cursor",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-wide-screen-bottom-cursor", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "RIGHT-11") || !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected initial wide screen with bottom prompt, got:\n%s", screen)
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(20, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	screenAfterShrink := host.Screen().String()
	if strings.Contains(screenAfterShrink, "RIGHT-11") {
		t.Fatalf("expected shrink to hide right edge on bottom-cursor screen, got:\n%s", screenAfterShrink)
	}
	_ = host.DrainRaw()

	host.Resize(60, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "RIGHT-11") || !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected expand to restore wide bottom-cursor screen without new input, got:\n%s", screen)
		}
		return nil
	})
	waitForRawContains(
		t,
		host,
		"RIGHT-11",
		2*time.Second,
		50*time.Millisecond,
		"expected Lingon to emit restored wide content after expand on bottom-cursor screen",
	)
}

func TestHostResizePreservesScrolledWideOutputWithoutInput(t *testing.T) {
	shell := preservedWideScrollOutputShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-scroll-output",
		SessionName: "viewport-preserve-wide-scroll-output",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-wide-scroll-output", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "ROW-30") || !strings.Contains(screen, "RIGHT-30") || !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected initial scrolled wide output with prompt, got:\n%s", screen)
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(20, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	screenAfterShrink := host.Screen().String()
	if strings.Contains(screenAfterShrink, "RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge on scrolled wide output, got:\n%s", screenAfterShrink)
	}
	_ = host.DrainRaw()

	host.Resize(60, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "RIGHT-30") || !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected expand to restore scrolled wide output without new input, got:\n%s", screen)
		}
		return nil
	})
	waitForRawContains(
		t,
		host,
		"RIGHT-30",
		2*time.Second,
		50*time.Millisecond,
		"expected Lingon to emit restored scrolled wide content after expand without new input",
	)
}

func TestHostResizePreservesScrolledWideOutputWithTabBarVisible(t *testing.T) {
	shell := preservedWideScrollOutputShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-scroll-output-tabs",
		SessionName: "viewport-preserve-wide-scroll-output-tabs",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-scroll-output-peer",
		SessionName: "viewport-preserve-wide-scroll-output-peer",
		Shell:       "/bin/sh",
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "viewport-preserve-wide-scroll-output-tabs") {
			return fmt.Errorf("expected tab bar visible before resize, got row=%q\nscreen:\n%s", row, host.Screen().String())
		}
		if !host.Screen().Contains("RIGHT-30") || !host.Screen().Contains("PROMPT>") {
			return fmt.Errorf("expected initial scrolled wide output with prompt, got:\n%s", host.Screen().String())
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(20, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	if host.Screen().Contains("RIGHT-30") {
		t.Fatalf("expected shrink to hide right edge with tab bar visible, got:\n%s", host.Screen().String())
	}
	_ = host.DrainRaw()

	host.Resize(60, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "RIGHT-30") || !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected expand to restore scrolled wide output with tab bar visible, got:\n%s", screen)
		}
		return nil
	})
	waitForRawContains(
		t,
		host,
		"RIGHT-30",
		2*time.Second,
		50*time.Millisecond,
		"expected Lingon to emit restored scrolled wide content after expand with tab bar visible",
	)
}

func TestHostResizePreservesWideContentInScrollbackWhileViewportIsNarrow(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-wide-scrollback",
		SessionName: "viewport-preserve-wide-scrollback",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-wide-scrollback", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("printf 'LEFT-1234567890-MID-abcdefghij-RIGHT-END\\n'\n")
	waitForStableSeededHostOutput(t, host, "RIGHT-END", 3*time.Second)
	host.Send("emit-lines FILL 2 20\n")
	waitForStableSeededHostOutput(t, host, "FILL-20", 3*time.Second)

	host.Resize(20, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte("g"))
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !host.Screen().Contains("LEFT-") {
		t.Fatalf("expected left edge of preserved row after entering scrollback, got:\n%s", host.Screen().String())
	}
	if host.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected right edge hidden before horizontal pan, got:\n%s", host.Screen().String())
	}

	for i := 0; i < 6; i++ {
		host.SendBytes([]byte("L"))
		advanceTestClock(h.Clock(), 40*time.Millisecond)
	}
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-END") {
			return ptytest.FormatRowDiff("host", 1, host.Screen().Row(1))
		}
		return nil
	})
}

func TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-post-shrink-scrollback",
		SessionName: "viewport-preserve-post-shrink-scrollback",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-post-shrink-scrollback", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("printf 'LEFT-1234567890-MID-abcdefghij-RIGHT-END\\n'\n")
	waitForStableSeededHostOutput(t, host, "RIGHT-END", 3*time.Second)

	host.Resize(20, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.Send("emit-lines FILL 2 20\n")
	waitForStableSeededHostOutput(t, host, "FILL-20", 3*time.Second)

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte("g"))
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !host.Screen().Contains("LEFT-") {
		t.Fatalf("expected left edge of preserved pre-shrink row after entering scrollback, got:\n%s", host.Screen().String())
	}
	if host.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected right edge hidden before horizontal pan, got:\n%s", host.Screen().String())
	}

	for i := 0; i < 6; i++ {
		host.SendBytes([]byte("L"))
		advanceTestClock(h.Clock(), 40*time.Millisecond)
	}

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(1)
		if !strings.Contains(row, "RIGHT-END") {
			return fmt.Errorf("expected preserved pre-shrink right edge after horizontal pan, row=%q\nscreen:\n%s", row, host.Screen().String())
		}
		if strings.Contains(row, "FILL-") {
			return fmt.Errorf("expected preserved pre-shrink row without post-shrink filler garbling, row=%q\nscreen:\n%s", row, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizePreservesLowerViewportContentAcrossShrinkAndExpand(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-preserve-lower-content",
		SessionName: "viewport-preserve-lower-content",
		Shell:       shell,
		Cols:        40,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	waitForHost(t, h, "viewport-preserve-lower-content", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("emit-lines KEEP 1 12\n")
	waitForStableSeededHostOutput(t, host, "KEEP-12", 3*time.Second)

	host.Resize(40, 6)
	advanceTestClock(host.Clock(), 200*time.Millisecond)

	if !host.Screen().Contains("KEEP-12") || !host.Screen().Contains("PROMPT>") {
		t.Fatalf("expected shrink to keep cursor-side lower rows visible, got:\n%s", host.Screen().String())
	}

	host.Resize(40, 12)
	advanceTestClock(host.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("KEEP-12") {
			return fmt.Errorf("expected restored lower viewport content after enlarging viewport, got:\n%s", host.Screen().String())
		}
		return nil
	})

	host.Resize(40, 6)
	advanceTestClock(host.Clock(), 200*time.Millisecond)
	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte("g"))
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	for i := 0; i < 3; i++ {
		host.SendBytes([]byte{0x1b, '[', '6', '~'}) // PgDn
		advanceTestClock(h.Clock(), 60*time.Millisecond)
	}

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "KEEP-3") || !strings.Contains(screen, "KEEP-8") {
			return fmt.Errorf("expected preserved hidden rows to remain reachable in scrollback, got:\n%s", screen)
		}
		return nil
	})
}
