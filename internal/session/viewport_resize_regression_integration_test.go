package session_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
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
		screen := host.Screen()
		if !strings.Contains(screen.String(), "RIGHT-11") || !strings.Contains(screen.String(), "PROMPT>") {
			return fmt.Errorf("expected expand to restore wide bottom-cursor screen without new input, got:\n%s", screen.String())
		}
		if cur := host.Cursor(); cur.Row != 12 {
			return fmt.Errorf("expected cursor on bottom row after expand, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(11), "PROMPT>") {
			return fmt.Errorf("expected prompt on bottom row after expand, got row=%q\nscreen:\n%s", screen.Row(11), screen.String())
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
		screen := host.Screen()
		if !strings.Contains(screen.String(), "RIGHT-30") || !strings.Contains(screen.String(), "PROMPT>") {
			return fmt.Errorf("expected expand to restore scrolled wide output without new input, got:\n%s", screen.String())
		}
		if cur := host.Cursor(); cur.Row != 12 {
			return fmt.Errorf("expected cursor on bottom row after expand, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(11), "PROMPT>") {
			return fmt.Errorf("expected prompt on bottom row after expand, got row=%q\nscreen:\n%s", screen.Row(11), screen.String())
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
		if strings.Contains(row, "connected to ") {
			return fmt.Errorf("waiting for transient connection banner to clear, row=%q", row)
		}
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
		screen := host.Screen()
		if !strings.Contains(screen.String(), "RIGHT-30") || !strings.Contains(screen.String(), "PROMPT>") {
			return fmt.Errorf("expected expand to restore scrolled wide output with tab bar visible, got:\n%s", screen.String())
		}
		if cur := host.Cursor(); cur.Row != 12 {
			return fmt.Errorf("expected cursor on bottom row after expand with tab bar visible, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(11), "PROMPT>") {
			return fmt.Errorf("expected prompt on bottom row after expand with tab bar visible, got row=%q\nscreen:\n%s", screen.Row(11), screen.String())
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

func TestHostResizeTypingWhileShrunkKeepsPromptOnBottomRowWithTabBarVisible(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const wideCommand = "clear; for i in $(seq 1 30); do printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\\n' \"$i\" \"$i\"; done\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-tabs-host",
		SessionName: "viewport-resize-typing-while-shrunk-tabs-host",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-tabs-peer",
		SessionName: "viewport-resize-typing-while-shrunk-tabs-peer",
		Shell:       "/bin/sh",
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)

	host.Send(wideCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("RIGHT-30-END") || !screen.Contains("PROMPT-002>") {
			return fmt.Errorf("expected initial scrolled wide output with prompt, got:\n%s", screen.String())
		}
		if !strings.Contains(screen.Row(0), "viewport-resize-typing-while-shrunk-tabs-host") {
			return fmt.Errorf("expected tab bar visible before shrink, got row=%q\nscreen:\n%s", screen.Row(0), screen.String())
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(40, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if screen.Contains("RIGHT-30-END") {
			return fmt.Errorf("expected shrink to hide right tail, got:\n%s", screen.String())
		}
		cur := host.Cursor()
		if cur.Row != 6 {
			return fmt.Errorf("expected cursor on bottom row while shrunk before typing, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(5), "PROMPT-002>") {
			return fmt.Errorf("expected prompt on bottom row while shrunk, got row=%q\nscreen:\n%s", screen.Row(5), screen.String())
		}
		return nil
	})

	baseline := host.Screen()
	host.Send("mkpod")

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != 6 {
			return fmt.Errorf("expected cursor to stay on bottom row while typing shrunk command, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		row0 := screen.Row(0)
		if got, want := row0, baseline.Row(0); got != want {
			return fmt.Errorf("expected top row to remain stable while typing shrunk command\nwant: %q\ngot:  %q\nscreen:\n%s", want, got, screen.String())
		}
		if strings.Contains(row0, "mkpod") {
			return fmt.Errorf("expected typed command not to bleed into top row, got row=%q\nscreen:\n%s", row0, screen.String())
		}
		bottom := screen.Row(5)
		if !strings.Contains(bottom, "PROMPT-002> mkpod") {
			return fmt.Errorf("expected typed command on bottom prompt row while shrunk, got row=%q\nscreen:\n%s", bottom, screen.String())
		}
		for i := 1; i < 5; i++ {
			if got, want := screen.Row(i), baseline.Row(i); got != want {
				return fmt.Errorf("expected shrunk viewport row %d to remain stable while typing command\nwant: %q\ngot:  %q\nscreen:\n%s", i+1, want, got, screen.String())
			}
		}
		return nil
	})
}

func TestHostResizeCtrlLClearAfterExpandClearsPreservedContent(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-clear-after-expand",
		SessionName: "viewport-resize-clear-after-expand",
		Shell:       sigwinchBashWrapper(t),
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-clear-after-expand-peer",
		SessionName: "viewport-resize-clear-after-expand-peer",
		Shell:       "/bin/sh",
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	host.Send("clear; ps aux\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected prompt after initial ps output, got:\n%s", screen)
		}
		if !strings.Contains(screen, "pts/") && !strings.Contains(screen, "/bin/bash") {
			return fmt.Errorf("expected initial process output on screen, got:\n%s", screen)
		}
		row := host.Screen().Row(0)
		if !strings.Contains(row, "viewport-resize-clear-after-expand") {
			return fmt.Errorf("expected tab bar visible before resize, got row=%q\nscreen:\n%s", row, screen)
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(40, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(100, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen().String()
		if !strings.Contains(screen, "PROMPT>") {
			return fmt.Errorf("expected expand to restore ps output, got:\n%s", screen)
		}
		if !strings.Contains(screen, "pts/") && !strings.Contains(screen, "/bin/bash") {
			return fmt.Errorf("expected expand to restore process output, got:\n%s", screen)
		}
		return nil
	})
	_ = host.DrainRaw()

	host.SendCtrlL()
	host.SendCtrlL()
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after clear, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		row := screen.Row(0)
		if strings.Contains(row, "viewport-resize-clear-after-expand") {
			return fmt.Errorf("expected tab bar hidden when prompt owns row 1, got row=%q\nscreen:\n%s", row, screen.String())
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt on row 1 after clear, got row=%q\nscreen:\n%s", row, screen.String())
		}
		if strings.Contains(screen.String(), "pts/") || strings.Contains(screen.String(), "ps aux") {
			return fmt.Errorf("expected clear to remove prior ps content after resize restore, got:\n%s", screen.String())
		}
		return nil
	})
}

func TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const wideCommand = "clear; for i in $(seq 1 30); do printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\\n' \"$i\" \"$i\"; done\n"
	var hostPTY bytes.Buffer

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-prompt-advance-host",
		SessionName: "viewport-resize-prompt-advance-host",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
		OnPTYRead: func(data []byte) {
			_, _ = hostPTY.Write(data)
		},
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-prompt-advance-control",
		SessionName: "viewport-resize-prompt-advance-control",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)
	waitForHostPromptNumber(t, control, 1, 3*time.Second)

	host.Send(wideCommand)
	control.Send(wideCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-30-END") || !control.Screen().Contains("RIGHT-30-END") {
			return fmt.Errorf("waiting for deterministic wide output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})
	waitForHostPromptNumber(t, host, 2, 3*time.Second)
	waitForHostPromptNumber(t, control, 2, 3*time.Second)
	_ = host.DrainRaw()
	_ = control.DrainRaw()

	host.Resize(40, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if host.Screen().Contains("RIGHT-30-END") {
		t.Fatalf("expected shrink to hide right edge, got:\n%s", host.Screen().String())
	}
	shrunkBeforeEnter := host.Screen().String()
	_ = host.DrainRaw()

	control.Send("\r")
	waitForHostPromptNumber(t, control, 3, 3*time.Second)
	controlScreen := control.Screen()

	hostPTY.Reset()
	host.Send("\r")
	waitForHostPromptNumber(t, host, 3, 3*time.Second)
	shrunkAfterEnter := host.Screen().String()
	_ = host.DrainRaw()

	host.Resize(100, 12)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !strings.Contains(screen.String(), "RIGHT-30-END") {
			return fmt.Errorf("expected expanded screen to restore right tail after shrunk prompt advance, got:\n%s", screen.String())
		}
		if !strings.Contains(screen.Row(0), "viewport-resize-prompt-advance-host") {
			return fmt.Errorf("expected tab bar visible after expand, got row=%q\nscreen:\n%s", screen.Row(0), screen.String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			screen,
			controlScreen,
			"viewport-resize-prompt-advance-host",
			"viewport-resize-prompt-advance-control",
		); err != nil {
			return fmt.Errorf("%v\npty:\n%q\nshrunk before enter:\n%s\nshrunk after enter:\n%s\nhost screen:\n%s\ncontrol screen:\n%s", err, hostPTY.String(), shrunkBeforeEnter, shrunkAfterEnter, screen.String(), controlScreen.String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected prompt-advance cursor to match wide control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, screen.String(), controlScreen.String())
		}
		return nil
	})
}

func TestHostResizeTypingAfterExpandPreservesPromptLine(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const wideCommand = "clear; for i in $(seq 1 30); do printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\\n' \"$i\" \"$i\"; done\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-after-expand-host",
		SessionName: "viewport-resize-typing-after-expand-host",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-after-expand-control",
		SessionName: "viewport-resize-typing-after-expand-control",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)
	waitForHostPromptNumber(t, control, 1, 3*time.Second)

	host.Send(wideCommand)
	control.Send(wideCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-30-END") || !control.Screen().Contains("RIGHT-30-END") {
			return fmt.Errorf("waiting for deterministic wide output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})
	waitForHostPromptNumber(t, host, 2, 3*time.Second)
	waitForHostPromptNumber(t, control, 2, 3*time.Second)
	_ = host.DrainRaw()
	_ = control.DrainRaw()

	host.Resize(40, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(100, 12)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-30-END") {
			return fmt.Errorf("expected expand to restore wide content, got:\n%s", host.Screen().String())
		}
		return nil
	})

	for _, ch := range []byte("ps aux") {
		host.SendBytes([]byte{ch})
		control.SendBytes([]byte{ch})
		advanceTestClock(h.Clock(), 100*time.Millisecond)
	}

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-typing-after-expand-host",
			"viewport-resize-typing-after-expand-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected typed-after-expand cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizeTypingWhileShrunkThenExpandPreservesCommandLine(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const wideCommand = "clear; for i in $(seq 1 30); do printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\\n' \"$i\" \"$i\"; done\n"
	const typedCommand = "echo TYPED-OK"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-host",
		SessionName: "viewport-resize-typing-while-shrunk-host",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-control",
		SessionName: "viewport-resize-typing-while-shrunk-control",
		Shell:       shell,
		Cols:        100,
		Rows:        12,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)
	waitForHostPromptNumber(t, control, 1, 3*time.Second)

	host.Send(wideCommand)
	control.Send(wideCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-30-END") || !control.Screen().Contains("RIGHT-30-END") {
			return fmt.Errorf("waiting for deterministic wide output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})
	waitForHostPromptNumber(t, host, 2, 3*time.Second)
	waitForHostPromptNumber(t, control, 2, 3*time.Second)
	_ = host.DrainRaw()
	_ = control.DrainRaw()

	host.Resize(40, 6)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	for _, ch := range []byte(typedCommand) {
		host.SendBytes([]byte{ch})
		advanceTestClock(h.Clock(), 100*time.Millisecond)
	}
	host.Send("\r")
	waitForRawContains(t, host, "TYPED-OK", 3*time.Second, 50*time.Millisecond, "expected host command output while shrunk")
	waitForHostPromptNumber(t, host, 3, 3*time.Second)

	control.Send(typedCommand + "\r")
	waitForRawContains(t, control, "TYPED-OK", 3*time.Second, 50*time.Millisecond, "expected control command output")
	waitForHostPromptNumber(t, control, 3, 3*time.Second)

	host.Resize(100, 12)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), "TYPED-OK") {
			return fmt.Errorf("expected expanded host screen to contain command output, got:\n%s", host.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-typing-while-shrunk-host",
			"viewport-resize-typing-while-shrunk-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected typed-while-shrunk cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizePostExpandFullScreenOutputMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const seedWide = "printf 'SEED-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-END\\n'\n"
	const fillCommand = "i=1; while [ $i -le 40 ]; do printf 'POST-%02d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%02d-END\\n' $i $i; i=$(($i+1)); done\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-post-expand-output-host",
		SessionName: "viewport-resize-post-expand-output-host",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-post-expand-output-control",
		SessionName: "viewport-resize-post-expand-output-control",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send(seedWide)
	control.Send(seedWide)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("RIGHT-END") || !control.Screen().Contains("RIGHT-END") {
			return fmt.Errorf("waiting for seeded wide content\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(40, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(100, 30)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send(fillCommand)
	control.Send(fillCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("POST-40") || !control.Screen().Contains("POST-40") {
			return fmt.Errorf("waiting for full-screen post-expand output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-post-expand-output-host",
			"viewport-resize-post-expand-output-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected post-expand full-screen cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizePsAuxAfterExpandKeepsPromptOnBottomRow(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const command = "clear; ps aux\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-ps-aux-host",
		SessionName: "viewport-resize-ps-aux-host",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-ps-aux-control",
		SessionName: "viewport-resize-ps-aux-control",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)
	waitForHostPromptNumber(t, control, 1, 3*time.Second)

	host.Send(command)
	control.Send(command)
	waitForHostPromptNumber(t, host, 2, 4*time.Second)
	waitForHostPromptNumber(t, control, 2, 4*time.Second)

	host.Resize(40, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(100, 30)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send(command)
	control.Send(command)
	waitForHostPromptNumber(t, host, 3, 4*time.Second)
	waitForHostPromptNumber(t, control, 3, 4*time.Second)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf(
				"expected host cursor to match control after ps aux post-resize, got host row=%d col=%d control row=%d col=%d\nhost:\n%s\ncontrol:\n%s",
				hostCur.Row,
				hostCur.Col,
				controlCur.Row,
				controlCur.Col,
				host.Screen().String(),
				control.Screen().String(),
			)
		}
		if hostCur.Row != 30 {
			return fmt.Errorf("expected host cursor on bottom row after ps aux post-resize, got row=%d col=%d\nhost:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		promptRow := host.Screen().Row(hostCur.Row - 1)
		if !strings.Contains(promptRow, "PROMPT-003>") {
			return fmt.Errorf("expected current prompt on bottom row after ps aux post-resize, got row=%q\nhost:\n%s", promptRow, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizePlainPsAuxAfterExpandKeepsPromptOnBottomRow(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const command = "ps aux\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-plain-ps-aux-host",
		SessionName: "viewport-resize-plain-ps-aux-host",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-plain-ps-aux-control",
		SessionName: "viewport-resize-plain-ps-aux-control",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)
	waitForHostPromptNumber(t, control, 1, 3*time.Second)

	host.Send(command)
	control.Send(command)
	waitForHostPromptNumber(t, host, 2, 4*time.Second)
	waitForHostPromptNumber(t, control, 2, 4*time.Second)

	host.Resize(40, 12)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(100, 30)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send(command)
	control.Send(command)
	waitForHostPromptNumber(t, host, 3, 4*time.Second)
	waitForHostPromptNumber(t, control, 3, 4*time.Second)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf(
				"expected host cursor to match control after plain ps aux post-resize, got host row=%d col=%d control row=%d col=%d\nhost:\n%s\ncontrol:\n%s",
				hostCur.Row,
				hostCur.Col,
				controlCur.Row,
				controlCur.Col,
				host.Screen().String(),
				control.Screen().String(),
			)
		}
		if hostCur.Row != 30 {
			return fmt.Errorf("expected host cursor on bottom row after plain ps aux post-resize, got row=%d col=%d\nhost:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		promptRow := host.Screen().Row(hostCur.Row - 1)
		if !strings.Contains(promptRow, "PROMPT-003>") {
			return fmt.Errorf("expected current prompt on bottom row after plain ps aux post-resize, got row=%q\nhost:\n%s", promptRow, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeTypingWhileShrunkAfterPsAuxKeepsPromptOnBottomRow(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const (
		wideCols   = 119
		wideRows   = 30
		shrunkCols = 80
		shrunkRows = 24
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-ps-host",
		SessionName: "viewport-resize-typing-while-shrunk-ps-host",
		Shell:       shell,
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-typing-while-shrunk-ps-peer",
		SessionName: "viewport-resize-typing-while-shrunk-ps-peer",
		Shell:       "/bin/sh",
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)

	host.Send("ps aux\n")
	waitForHostPromptNumber(t, host, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("ps aux") || !screen.Contains("PROMPT-002>") {
			return fmt.Errorf("expected ps aux output with prompt before shrink, got:\n%s", screen.String())
		}
		return nil
	})
	_ = host.DrainRaw()

	host.Resize(shrunkCols, shrunkRows)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != shrunkRows {
			return fmt.Errorf("expected cursor on bottom row while shrunk after ps aux, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(shrunkRows-1), "PROMPT-002>") {
			return fmt.Errorf("expected prompt on bottom row while shrunk after ps aux, got row=%q\nscreen:\n%s", screen.Row(shrunkRows-1), screen.String())
		}
		return nil
	})

	baseline := host.Screen()
	host.Send("mkpod")

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != shrunkRows {
			return fmt.Errorf("expected cursor to stay on bottom row while typing after shrunk ps aux, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if got, want := screen.Row(0), baseline.Row(0); got != want {
			return fmt.Errorf("expected top row to remain stable while typing after shrunk ps aux\nwant: %q\ngot:  %q\nscreen:\n%s", want, got, screen.String())
		}
		if strings.Contains(screen.Row(0), "mkpod") {
			return fmt.Errorf("expected typed command not to bleed into top row after shrunk ps aux, got row=%q\nscreen:\n%s", screen.Row(0), screen.String())
		}
		if !strings.Contains(screen.Row(shrunkRows-1), "PROMPT-002> mkpod") {
			return fmt.Errorf("expected typed command on bottom prompt row after shrunk ps aux, got row=%q\nscreen:\n%s", screen.Row(shrunkRows-1), screen.String())
		}
		return nil
	})
}

func TestHostResizeWhileShrunkAfterPsAuxMatchesBottomViewportCrop(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const (
		wideCols   = 119
		wideRows   = 30
		shrunkCols = 80
		shrunkRows = 24
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-while-shrunk-ps-host",
		SessionName: "viewport-resize-while-shrunk-ps-host",
		Shell:       shell,
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-while-shrunk-ps-peer",
		SessionName: "viewport-resize-while-shrunk-ps-peer",
		Shell:       "/bin/sh",
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)

	host.Send("ps aux\n")
	waitForHostPromptNumber(t, host, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("ps aux") || !screen.Contains("PROMPT-002>") {
			return fmt.Errorf("expected ps aux output with prompt before shrink, got:\n%s", screen.String())
		}
		return nil
	})

	host.Resize(shrunkCols, shrunkRows)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != shrunkRows {
			return fmt.Errorf("expected cursor on bottom row while shrunk after ps aux, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(shrunkRows-1), "PROMPT-002>") {
			return fmt.Errorf("expected prompt on bottom row while shrunk after ps aux, got row=%q\nscreen:\n%s", screen.Row(shrunkRows-1), screen.String())
		}
		return nil
	})
}

func TestHostResizeWhileShrunkWithDecoratedPromptKeepsPromptVisible(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := decoratedPromptBash(t)
	const (
		wideCols   = 119
		wideRows   = 30
		shrunkCols = 80
		shrunkRows = 24
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-decorated-prompt-host",
		SessionName: "viewport-resize-decorated-prompt-host",
		Shell:       shell,
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-decorated-prompt-peer",
		SessionName: "viewport-resize-decorated-prompt-peer",
		Shell:       "/bin/sh",
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), "~{}") {
			return fmt.Errorf("waiting for decorated prompt\nscreen:\n%s", host.Screen().String())
		}
		return nil
	})

	host.Send("ps aux\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("ps aux") || !screen.Contains("~{}") {
			return fmt.Errorf("expected ps aux output with decorated prompt before shrink, got:\n%s", screen.String())
		}
		return nil
	})

	host.Resize(shrunkCols, shrunkRows)
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != shrunkRows {
			return fmt.Errorf("expected cursor on bottom row while shrunk with decorated prompt, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if !strings.Contains(screen.Row(shrunkRows-1), "~{}") {
			return fmt.Errorf("expected decorated prompt on bottom row while shrunk, got row=%q\nscreen:\n%s", screen.Row(shrunkRows-1), screen.String())
		}
		return nil
	})
}

func TestHostResizeWhileShrunkAfterPsAuxKeepsPromptVisibleAcrossViewportSizes(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	cases := []struct {
		name string
		cols int
		rows int
	}{
		{name: "80x24", cols: 80, rows: 24},
		{name: "80x20", cols: 80, rows: 20},
		{name: "72x18", cols: 72, rows: 18},
		{name: "60x16", cols: 60, rows: 16},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			host := h.StartHost(ptytest.HostOptions{
				SessionID:   "viewport-resize-matrix-host-" + strings.ReplaceAll(tc.name, "x", "-"),
				SessionName: "viewport-resize-matrix-host-" + strings.ReplaceAll(tc.name, "x", "-"),
				Shell:       shell,
				Cols:        119,
				Rows:        30,
			})
			t.Cleanup(host.Cancel)

			peer := h.StartHost(ptytest.HostOptions{
				SessionID:   "viewport-resize-matrix-peer-" + strings.ReplaceAll(tc.name, "x", "-"),
				SessionName: "viewport-resize-matrix-peer-" + strings.ReplaceAll(tc.name, "x", "-"),
				Shell:       "/bin/sh",
				Cols:        119,
				Rows:        30,
			})
			t.Cleanup(peer.Cancel)

			waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
			waitForHostPromptNumber(t, host, 1, 3*time.Second)

			host.Send("ps aux\n")
			waitForHostPromptNumber(t, host, 2, 4*time.Second)
			eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
				screen := host.Screen()
				if !screen.Contains("ps aux") || !screen.Contains("PROMPT-002>") {
					return fmt.Errorf("expected ps aux output with prompt before shrink, got:\n%s", screen.String())
				}
				return nil
			})

			host.Resize(tc.cols, tc.rows)
			advanceTestClock(h.Clock(), 200*time.Millisecond)

			eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
				screen := host.Screen()
				cur := host.Cursor()
				if cur.Row != tc.rows {
					return fmt.Errorf("expected cursor on bottom row while shrunk at %s, got row=%d col=%d\nscreen:\n%s", tc.name, cur.Row, cur.Col, screen.String())
				}
				if !strings.Contains(screen.Row(tc.rows-1), "PROMPT-002>") {
					return fmt.Errorf("expected prompt on bottom row while shrunk at %s, got row=%q\nscreen:\n%s", tc.name, screen.Row(tc.rows-1), screen.String())
				}
				return nil
			})
		})
	}
}

func TestHostResizeImmediateTypingAfterShrinkKeepsPromptOnBottomRow(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const (
		wideCols   = 119
		wideRows   = 30
		shrunkCols = 80
		shrunkRows = 24
	)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-immediate-typing-after-shrink-host",
		SessionName: "viewport-resize-immediate-typing-after-shrink-host",
		Shell:       shell,
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(host.Cancel)

	peer := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-immediate-typing-after-shrink-peer",
		SessionName: "viewport-resize-immediate-typing-after-shrink-peer",
		Shell:       "/bin/sh",
		Cols:        wideCols,
		Rows:        wideRows,
	})
	t.Cleanup(peer.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForHostPromptNumber(t, host, 1, 3*time.Second)

	host.Send("ps aux\n")
	waitForHostPromptNumber(t, host, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if !screen.Contains("ps aux") || !screen.Contains("PROMPT-002>") {
			return fmt.Errorf("expected ps aux output with prompt before immediate shrink typing, got:\n%s", screen.String())
		}
		return nil
	})

	host.Resize(shrunkCols, shrunkRows)
	host.Send("mkpod")

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		cur := host.Cursor()
		if cur.Row != shrunkRows {
			return fmt.Errorf("expected cursor on bottom row after immediate typing post-shrink, got row=%d col=%d\nscreen:\n%s", cur.Row, cur.Col, screen.String())
		}
		if strings.Contains(screen.Row(0), "mkpod") {
			return fmt.Errorf("expected immediate typed command not to bleed into top row after shrink, got row=%q\nscreen:\n%s", screen.Row(0), screen.String())
		}
		if !strings.Contains(screen.Row(shrunkRows-1), "PROMPT-002> mkpod") {
			return fmt.Errorf("expected immediate typed command on bottom prompt row after shrink, got row=%q\nscreen:\n%s", screen.Row(shrunkRows-1), screen.String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportClearAfterExpandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const cols = 119
	const rows = 62

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-host",
		SessionName: "viewport-resize-large-clear-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-control",
		SessionName: "viewport-resize-large-clear-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send("ps aux\n")
	control.Send("ps aux\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("ps aux") || !control.Screen().Contains("ps aux") {
			return fmt.Errorf("waiting for ps aux output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("clear\n")
	control.Send("clear\n")
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-clear-host",
			"viewport-resize-large-clear-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected large clear cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		if hostCur.Row != 1 {
			return fmt.Errorf("expected large clear cursor on row 1, got row=%d col=%d\nhost screen:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportCtrlLLClearAfterExpandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const cols = 119
	const rows = 62

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-ctrl-l-clear-host",
		SessionName: "viewport-resize-large-ctrl-l-clear-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-ctrl-l-clear-control",
		SessionName: "viewport-resize-large-ctrl-l-clear-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send("ps aux\n")
	control.Send("ps aux\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("ps aux") || !control.Screen().Contains("ps aux") {
			return fmt.Errorf("waiting for ps aux output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.SendCtrlL()
	host.Send("l")
	control.SendCtrlL()
	control.Send("l")
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-ctrl-l-clear-host",
			"viewport-resize-large-ctrl-l-clear-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected large ctrl+l l clear cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		if hostCur.Row != 1 {
			return fmt.Errorf("expected large ctrl+l l clear cursor on row 1, got row=%d col=%d\nhost screen:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportFullScreenClearAfterExpandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := resizeClearCommandShell(t)
	const cols = 119
	const rows = 62

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-full-clear-host",
		SessionName: "viewport-resize-large-full-clear-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-full-clear-control",
		SessionName: "viewport-resize-large-full-clear-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), host, 1, 3*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 1, 3*time.Second)

	host.Send("fill\n")
	control.Send("fill\n")
	waitForResizeClearPrompt(t, h.Clock(), host, 2, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)
	_ = host.DrainRaw()
	_ = control.DrainRaw()

	host.Send("clear\n")
	control.Send("clear\n")
	hostRaw := waitForRawChunkContains(t, host, "\x1b[2J", 2*time.Second, 50*time.Millisecond, "expected resized host clear to trigger full-screen clear sequence")
	controlRaw := waitForRawChunkContains(t, control, "\x1b[2J", 2*time.Second, 50*time.Millisecond, "expected control clear to trigger full-screen clear sequence")
	waitForHostPromptNumber(t, host, 3, 4*time.Second)
	waitForHostPromptNumber(t, control, 3, 4*time.Second)
	_ = hostRaw
	_ = controlRaw

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-full-clear-host",
			"viewport-resize-large-full-clear-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected full-screen clear cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		if hostCur.Row != 1 {
			return fmt.Errorf("expected full-screen clear cursor on row 1, got row=%d col=%d\nhost screen:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportFullScreenCtrlLLClearAfterExpandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const cols = 119
	const rows = 62
	const fillCommand = "clear; i=1; while [ $i -le 120 ]; do printf 'FILL-%03d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%03d-END\\n' $i $i; i=$(($i+1)); done\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-full-ctrl-l-clear-host",
		SessionName: "viewport-resize-large-full-ctrl-l-clear-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-full-ctrl-l-clear-control",
		SessionName: "viewport-resize-large-full-ctrl-l-clear-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send(fillCommand)
	control.Send(fillCommand)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") || !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.SendCtrlL()
	host.Send("l")
	control.SendCtrlL()
	control.Send("l")
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for prompt after ctrl+l l clear\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-full-ctrl-l-clear-host",
			"viewport-resize-large-full-ctrl-l-clear-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected full-screen ctrl+l l clear cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		if hostCur.Row != 1 {
			return fmt.Errorf("expected full-screen ctrl+l l clear cursor on row 1, got row=%d col=%d\nhost screen:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportClearThenShortCommandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const cols = 119
	const rows = 62
	const fillCommand = "clear; i=1; while [ $i -le 120 ]; do printf 'FILL-%03d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%03d-END\\n' $i $i; i=$(($i+1)); done\n"
	const shortCommand = "printf 'SHORT-ONE\\nSHORT-TWO\\nSHORT-THREE\\n'\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-short-host",
		SessionName: "viewport-resize-large-clear-short-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-short-control",
		SessionName: "viewport-resize-large-clear-short-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send(fillCommand)
	control.Send(fillCommand)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("clear\n")
	control.Send("clear\n")
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send(shortCommand)
	control.Send(shortCommand)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("SHORT-THREE") || !control.Screen().Contains("SHORT-THREE") {
			return fmt.Errorf("waiting for short command output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-clear-short-host",
			"viewport-resize-large-clear-short-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected clear-then-short cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportCtrlLLClearThenShortCommandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("PS1", "PROMPT> ")
	shell := sigwinchBashWrapper(t)
	const cols = 119
	const rows = 62
	const fillCommand = "clear; i=1; while [ $i -le 120 ]; do printf 'FILL-%03d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%03d-END\\n' $i $i; i=$(($i+1)); done\n"
	const shortCommand = "printf 'SHORT-ONE\\nSHORT-TWO\\nSHORT-THREE\\n'\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-ctrl-l-clear-short-host",
		SessionName: "viewport-resize-large-ctrl-l-clear-short-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-ctrl-l-clear-short-control",
		SessionName: "viewport-resize-large-ctrl-l-clear-short-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT>") || !control.Screen().Contains("PROMPT>") {
			return fmt.Errorf("waiting for initial prompts\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Send(fillCommand)
	control.Send(fillCommand)
	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.SendCtrlL()
	host.Send("l")
	control.SendCtrlL()
	control.Send("l")
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send(shortCommand)
	control.Send(shortCommand)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("SHORT-THREE") || !control.Screen().Contains("SHORT-THREE") {
			return fmt.Errorf("waiting for short command output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-ctrl-l-clear-short-host",
			"viewport-resize-large-ctrl-l-clear-short-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected ctrl+l l clear-then-short cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportClearWhileShrunkThenExpandMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := resizeClearCommandShell(t)
	const cols = 119
	const rows = 62

	h := newHarness(t)
	var hostPTY bytes.Buffer
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-while-shrunk-host",
		SessionName: "viewport-resize-large-clear-while-shrunk-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
		OnPTYRead: func(data []byte) {
			_, _ = hostPTY.Write(data)
		},
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-while-shrunk-control",
		SessionName: "viewport-resize-large-clear-while-shrunk-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), host, 1, 3*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 1, 3*time.Second)

	host.Send("fill\n")
	control.Send("fill\n")
	waitForResizeClearPrompt(t, h.Clock(), host, 2, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("clear\n")
	control.Send("clear\n")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(hostPTY.String(), "PROMPT-003>") {
			return fmt.Errorf("waiting for raw prompt after shrink-clear\nraw:\n%s", hostPTY.String())
		}
		return nil
	})
	waitForResizeClearPrompt(t, h.Clock(), host, 3, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 3, 4*time.Second)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-clear-while-shrunk-host",
			"viewport-resize-large-clear-while-shrunk-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected shrink-clear-expand cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		if !strings.Contains(host.Screen().Row(hostCur.Row-1), "PROMPT-003>") {
			return fmt.Errorf("expected prompt after shrink-clear-expand on cursor row, got row=%q\nhost screen:\n%s", host.Screen().Row(hostCur.Row-1), host.Screen().String())
		}
		return nil
	})
}

func TestHostResizeLargeViewportClearWhileShrunkThenExpandPsAuxMatchesControl(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	shell := resizeClearCommandShell(t)
	const cols = 119
	const rows = 62

	h := newHarness(t)
	var hostPTY bytes.Buffer
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-while-shrunk-ps-host",
		SessionName: "viewport-resize-large-clear-while-shrunk-ps-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
		OnPTYRead: func(data []byte) {
			_, _ = hostPTY.Write(data)
		},
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-large-clear-while-shrunk-ps-control",
		SessionName: "viewport-resize-large-clear-while-shrunk-ps-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), host, 1, 3*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 1, 3*time.Second)

	host.Send("fill\n")
	control.Send("fill\n")
	waitForResizeClearPrompt(t, h.Clock(), host, 2, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 2, 4*time.Second)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("clear\n")
	control.Send("clear\n")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(hostPTY.String(), "PROMPT-003>") {
			return fmt.Errorf("waiting for raw prompt after shrink-clear\nraw:\n%s", hostPTY.String())
		}
		return nil
	})
	waitForResizeClearPrompt(t, h.Clock(), host, 3, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 3, 4*time.Second)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("ps aux\n")
	control.Send("ps aux\n")
	waitForResizeClearPrompt(t, h.Clock(), host, 4, 4*time.Second)
	waitForResizeClearPrompt(t, h.Clock(), control, 4, 4*time.Second)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PS-THREE") || !control.Screen().Contains("PS-THREE") {
			return fmt.Errorf("waiting for deterministic ps output after shrink-clear-expand\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-large-clear-while-shrunk-ps-host",
			"viewport-resize-large-clear-while-shrunk-ps-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		controlCur := control.Cursor()
		if hostCur.Row != controlCur.Row || hostCur.Col != controlCur.Col {
			return fmt.Errorf("expected shrink-clear-expand-ps cursor to match control, got host row=%d col=%d control row=%d col=%d\nhost screen:\n%s\ncontrol screen:\n%s", hostCur.Row, hostCur.Col, controlCur.Row, controlCur.Col, host.Screen().String(), control.Screen().String())
		}
		return nil
	})
}

func TestHostResizeBashClearWhileShrunkThenExpandMatchesControl(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := countingPromptBash(t)
	const cols = 119
	const rows = 62
	const fillCommand = "clear; i=1; while [ $i -le 120 ]; do printf 'FILL-%03d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%03d-END\\n' $i $i; i=$(($i+1)); done\n"

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-bash-clear-while-shrunk-host",
		SessionName: "viewport-resize-bash-clear-while-shrunk-host",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(host.Cancel)

	control := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-resize-bash-clear-while-shrunk-control",
		SessionName: "viewport-resize-bash-clear-while-shrunk-control",
		Shell:       shell,
		Cols:        cols,
		Rows:        rows,
	})
	t.Cleanup(control.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForConnectedBannerClear(t, control, 4*time.Second)

	host.Send(fillCommand)
	control.Send(fillCommand)
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("FILL-120") || !control.Screen().Contains("FILL-120") {
			return fmt.Errorf("waiting for full-height bash output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		nums := promptNumbersFromScreen(host.Screen().String())
		if len(nums) == 0 || nums[len(nums)-1] < 2 {
			return fmt.Errorf("waiting for second host prompt\nhost:\n%s", host.Screen().String())
		}
		nums = promptNumbersFromScreen(control.Screen().String())
		if len(nums) == 0 || nums[len(nums)-1] < 2 {
			return fmt.Errorf("waiting for second control prompt\ncontrol:\n%s", control.Screen().String())
		}
		return nil
	})

	host.Resize(80, 24)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	host.Send("clear\n")
	control.Send("clear\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("PROMPT-003>") || !control.Screen().Contains("PROMPT-003>") {
			return fmt.Errorf("waiting for third prompt after clear\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		return nil
	})

	host.Resize(cols, rows)
	advanceTestClock(h.Clock(), 250*time.Millisecond)

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-bash-clear-while-shrunk-host",
			"viewport-resize-bash-clear-while-shrunk-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		hostCur := host.Cursor()
		if hostCur.Row != 1 {
			return fmt.Errorf("expected bash clear cursor on row 1 after expand, got row=%d col=%d\nhost screen:\n%s", hostCur.Row, hostCur.Col, host.Screen().String())
		}
		return nil
	})

	host.Send("printf 'AFTER-CLEAR\\n'\n")
	control.Send("printf 'AFTER-CLEAR\\n'\n")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !host.Screen().Contains("AFTER-CLEAR") || !control.Screen().Contains("AFTER-CLEAR") {
			return fmt.Errorf("waiting for post-clear short output\nhost:\n%s\ncontrol:\n%s", host.Screen().String(), control.Screen().String())
		}
		if err := compareScreensWithNormalizedTabTitles(
			host.Screen(),
			control.Screen(),
			"viewport-resize-bash-clear-while-shrunk-host",
			"viewport-resize-bash-clear-while-shrunk-control",
		); err != nil {
			return fmt.Errorf("%v\nhost screen:\n%s\ncontrol screen:\n%s", err, host.Screen().String(), control.Screen().String())
		}
		if host.Screen().Contains("FILL-120") {
			return fmt.Errorf("expected clear to remove pre-clear filler rows, got:\n%s", host.Screen().String())
		}
		return nil
	})
}

func compareScreensWithNormalizedTabTitles(got, want ptytest.Screen, gotTitle, wantTitle string) error {
	gotLines := append([]string(nil), got.Lines...)
	wantLines := append([]string(nil), want.Lines...)
	if len(gotLines) != len(wantLines) {
		return fmt.Errorf("expected same row count, got %d vs %d", len(gotLines), len(wantLines))
	}
	for row := range gotLines {
		gotLine := gotLines[row]
		wantLine := wantLines[row]
		if row <= 1 {
			gotLine = normalizeViewportResizeTabRow(gotLine, gotTitle, wantTitle)
			wantLine = normalizeViewportResizeTabRow(wantLine, gotTitle, wantTitle)
		}
		if gotLine != wantLine {
			return fmt.Errorf("screen mismatch at row %d\nhost:    %q\ncontrol: %q", row+1, gotLine, wantLine)
		}
	}
	return nil
}

var viewportResizeTabTokenRe = regexp.MustCompile(`viewport-resize[^ ]*`)

func normalizeViewportResizeTabRow(line string, titles ...string) string {
	const banner = "connected to "
	prefix := line
	suffix := ""
	if idx := strings.Index(line, banner); idx >= 0 {
		prefix = line[:idx]
		suffix = line[idx:]
	}
	for _, title := range titles {
		prefix = strings.ReplaceAll(prefix, title, "<SESSION>")
	}
	prefix = viewportResizeTabTokenRe.ReplaceAllString(prefix, "<SESSION>")
	return prefix + suffix
}

func resizeClearCommandShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resize-clear-command-shell.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

count=1
line=''

draw_prompt() {
  printf 'PROMPT-%03d> ' "$count"
}

emit_fill() {
  printf '\033[H\033[2J'
  i=1
  while [ $i -le 120 ]; do
    printf 'FILL-%03d-LEFT-1234567890-MID-abcdefghijklmnopqrstuvwxyz0123456789-RIGHT-%03d-END\r\n' "$i" "$i"
    i=$((i+1))
  done
}

emit_ps() {
  printf 'PS-ONE\r\nPS-TWO\r\nPS-THREE\r\n'
}

run_line() {
  case "$line" in
    fill)
      emit_fill
      ;;
    clear)
      printf '\033[H\033[2J'
      ;;
    "ps aux")
      emit_ps
      ;;
    "")
      printf '\r\n'
      ;;
    *)
      printf '\r\nUNKNOWN:%s\r\n' "$line"
      ;;
  esac
  line=''
  count=$((count+1))
  draw_prompt
}

draw_prompt
while IFS= read -rsn1 ch; do
  if [ -z "$ch" ]; then
    run_line
    continue
  fi
  case "$ch" in
    $'\r'|$'\n')
      run_line
      ;;
    *)
      line+="$ch"
      printf '%s' "$ch"
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write resize clear shell: %v", err)
	}
	return path
}

func decoratedPromptBash(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "bashrc")
	wrapperPath := filepath.Join(dir, "bash-wrapper.sh")
	const rc = `
update_prompt() {
  printf '\033]0;decorated-shell\007'
}
PROMPT_COMMAND=update_prompt
PS1='\[\e[32m\]\u\[\e[0m\] ~{} '
set +o emacs
set +o vi
`
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatalf("write decorated bashrc: %v", err)
	}
	wrapper := fmt.Sprintf("#!/usr/bin/env bash\nexec /bin/bash --noprofile --rcfile %q -i\n", rcPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write decorated bash wrapper: %v", err)
	}
	return wrapperPath
}

func waitForResizeClearPrompt(t *testing.T, clk clock.Clock, sess *ptytest.PTYSession, want int, timeout time.Duration) {
	t.Helper()
	eventuallyWithClock(t, clk, timeout, 50*time.Millisecond, func() error {
		nums := promptNumbersFromScreen(sess.Screen().String())
		if len(nums) == 0 {
			return fmt.Errorf("waiting for numbered prompt %d\nscreen:\n%s", want, sess.Screen().String())
		}
		if nums[len(nums)-1] != want {
			return fmt.Errorf("waiting for prompt %d, got %v\nscreen:\n%s", want, nums, sess.Screen().String())
		}
		return nil
	})
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
