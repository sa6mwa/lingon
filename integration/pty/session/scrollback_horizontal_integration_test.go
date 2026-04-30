//go:build integration
// +build integration

package integrationptysession_test

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostScrollbackHorizontalPanAndHomeReset(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "scroll_host_horizontal_pan",
		SessionName: "scroll_host_horizontal_pan",
		Shell:       shell,
		Cols:        60,
		Rows:        12,
	})

	waitForHost(t, h, "scroll_host_horizontal_pan", 3*time.Second)
	waitForConnectedBannerClear(t, host, 4*time.Second)
	waitForHostPromptIdle(t, host, 3*time.Second, 50*time.Millisecond, 3)

	host.Send("printf 'LEFT-1234567890-MID-abcdefghij-RIGHT-END\\n'\n")
	waitForStableSeededHostOutput(t, host, "RIGHT-END", 3*time.Second)
	host.Send("emit-lines FILL 2 20\n")
	waitForStableSeededHostOutput(t, host, "FILL-20", 3*time.Second)

	host.Resize(20, 12)
	advanceTestClock(h.Clock(), 150*time.Millisecond)

	host.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	host.SendBytes([]byte("g"))
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !host.Screen().Contains("LEFT-") {
		t.Fatalf("expected left edge of wide scrollback row after entering scrollback, got:\n%s", host.Screen().String())
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

	host.SendBytes([]byte{0x1b, '[', 'H'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !host.Screen().Contains("LEFT-") || host.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected Home to reset horizontal pan, got:\n%s", host.Screen().String())
	}
}

func TestRemoteViewportFollowsCursorAndScrollbackPansHorizontally(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := scrollbackShell(t)

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "remote-wide-source",
		SessionName: "remote-wide-source",
		Shell:       shell,
		Cols:        80,
		Rows:        12,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "remote-narrow-viewer",
		SessionName: "remote-narrow-viewer",
		Shell:       shell,
		Cols:        20,
		Rows:        12,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

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

	hostB.Send("echo 12345678901234567890TAIL")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !hostB.Screen().Contains("TAIL") {
			return ptytest.FormatRowDiff("viewer", 11, hostB.Screen().Row(11))
		}
		return nil
	})

	hostB.SendBytes([]byte{'\r'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	hostB.Send("printf 'LEFT-1234567890-MID-abcdefghij-RIGHT-END\\n'\n")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !hostA.Screen().Contains("RIGHT-END") {
			return ptytest.FormatRowDiff("source", 0, hostA.Screen().Row(0))
		}
		return nil
	})
	hostB.Send("emit-lines FILL 2 20\n")
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !hostA.Screen().Contains("FILL-20") {
			return ptytest.FormatRowDiff("source", 0, hostA.Screen().Row(0))
		}
		return nil
	})

	hostB.SendBytes([]byte{0x0c, '['})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	hostB.SendBytes([]byte("g"))
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !hostB.Screen().Contains("LEFT-") {
		t.Fatalf("expected left edge of remote scrollback row after entering scrollback, got:\n%s", hostB.Screen().String())
	}
	if hostB.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected right edge hidden before horizontal pan on remote scrollback, got:\n%s", hostB.Screen().String())
	}

	for i := 0; i < 6; i++ {
		hostB.SendBytes([]byte("L"))
		advanceTestClock(h.Clock(), 40*time.Millisecond)
	}
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !hostB.Screen().Contains("RIGHT-END") {
			return ptytest.FormatRowDiff("viewer", 1, hostB.Screen().Row(1))
		}
		return nil
	})

	hostB.SendBytes([]byte{0x1b, '[', 'H'})
	advanceTestClock(h.Clock(), 120*time.Millisecond)
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !hostB.Screen().Contains("LEFT-") || hostB.Screen().Contains("RIGHT-END") {
		t.Fatalf("expected Home to reset horizontal pan on remote scrollback, got:\n%s", hostB.Screen().String())
	}
}
