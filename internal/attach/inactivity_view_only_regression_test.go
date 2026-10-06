package attach_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/ptytest"
)

type attachRecordingNotifier struct {
	mu       sync.Mutex
	requests []desktopnotify.Request
}

func (n *attachRecordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.mu.Lock()
	n.requests = append(n.requests, req)
	n.mu.Unlock()
	return nil
}

func (n *attachRecordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.requests)
}

func startInactivityHost(t *testing.T, h *ptytest.Harness, opts ptytest.HostOptions) *ptytest.PTYSession {
	t.Helper()
	// This fixture needs line input and observable output, with no dependence
	// on the engineer's shell profile or its startup probes.
	path := filepath.Join(t.TempDir(), "inactivity-shell.sh")
	script := `#!/bin/sh
set -eu
stty -echo
printf '\nINACTIVITY-SHELL-READY\n'
while IFS= read -r line; do
  case "$line" in
    'echo '*) printf '%s\n' "${line#echo }" ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	opts.Shell = path
	host := h.StartHost(opts)
	t.Cleanup(host.Cancel)
	host.Eventually(3*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("INACTIVITY-SHELL-READY") {
			return fmt.Errorf("owned inactivity shell is not ready; screen:\n%s", screen.String())
		}
		return nil
	})
	return host
}

func advanceInactivityClocks(h *ptytest.Harness, wallClock *clock.MockClock, duration time.Duration) {
	for elapsed := time.Duration(0); elapsed < duration; elapsed += 50 * time.Millisecond {
		wallClock.Add(50 * time.Millisecond)
		h.Advance(50 * time.Millisecond)
	}
}

func TestAttachConnectDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	wallClock := clock.NewMock()
	h := newHarness(t, ptytest.WithWallClock(wallClock), ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &attachRecordingNotifier{}

	host := startInactivityHost(t, h, ptytest.HostOptions{
		SessionID:       "attach-wall-connect",
		SessionName:     "attach-wall-connect",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-connect"})
	host.Send("echo ATTACH_WALL_CONNECT_READY\n")
	if !screenContainsWithin(host, "ATTACH_WALL_CONNECT_READY", 2*time.Second) {
		t.Fatalf("expected source session marker before enabling inactivity")
	}

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}
	advanceInactivityClocks(h, wallClock, time.Second)

	host.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		if notifier.count() != 1 {
			return fmt.Errorf("waiting for first inactivity notification")
		}
		return nil
	})

	attachSess := h.StartAttach(ptytest.AttachOptions{
		SessionID: "attach-wall-connect",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	if !screenContainsWithin(attachSess, "ATTACH_WALL_CONNECT_READY", 3*time.Second) {
		t.Fatalf("expected attach to render source session after connect")
	}

	advanceInactivityClocks(h, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected attach connect/replay to avoid rearming inactivity, got %d notifications", got)
	}
	host.Send("echo ATTACH_INPUT_REARMS\n")
	if !screenContainsWithin(host, "ATTACH_INPUT_REARMS", 2*time.Second) {
		t.Fatal("actual terminal input was not delivered")
	}
	advanceInactivityClocks(h, wallClock, 1500*time.Millisecond)
	host.Eventually(time.Second, 10*time.Millisecond, func(ptytest.Screen) error {
		if notifier.count() != 2 {
			return fmt.Errorf("actual input must rearm inactivity, got %d notifications", notifier.count())
		}
		return nil
	})
}

func TestMultiAttachTabSwitchDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	wallClock := clock.NewMock()
	h := newHarness(t, ptytest.WithWallClock(wallClock), ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &attachRecordingNotifier{}

	hostA := startInactivityHost(t, h, ptytest.HostOptions{
		SessionID:       "attach-wall-switch-a",
		SessionName:     "attach-wall-switch-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})

	hostB := startInactivityHost(t, h, ptytest.HostOptions{
		SessionID:   "attach-wall-switch-b",
		SessionName: "attach-wall-switch-b",
		Cols:        100,
		Rows:        30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-switch-a", "attach-wall-switch-b"})
	hostA.Send("echo ATTACH_WALL_SWITCH_A\n")
	hostB.Send("echo ATTACH_WALL_SWITCH_B\n")
	if !screenContainsWithin(hostA, "ATTACH_WALL_SWITCH_A", 2*time.Second) {
		t.Fatalf("expected source session marker before enabling inactivity")
	}
	if !screenContainsWithin(hostB, "ATTACH_WALL_SWITCH_B", 2*time.Second) {
		t.Fatalf("expected peer session marker before attach")
	}

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}
	advanceInactivityClocks(h, wallClock, time.Second)

	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		if notifier.count() != 1 {
			return fmt.Errorf("waiting for first inactivity notification")
		}
		return nil
	})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-wall-switch-b",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	if !screenContainsWithin(attachSess, "ATTACH_WALL_SWITCH_B", 3*time.Second) {
		t.Fatalf("expected multi-attach to render initial session before tab switch")
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")
	if !screenContainsWithin(attachSess, "ATTACH_WALL_SWITCH_A", 3*time.Second) {
		t.Fatalf("expected multi-attach tab switch to render monitored session")
	}

	advanceInactivityClocks(h, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected multi-attach tab switch to avoid rearming inactivity, got %d notifications", got)
	}
	hostA.Send("echo TAB_INPUT_REARMS\n")
	if !screenContainsWithin(hostA, "TAB_INPUT_REARMS", 2*time.Second) {
		t.Fatal("actual terminal input was not delivered")
	}
	advanceInactivityClocks(h, wallClock, 1500*time.Millisecond)
	hostA.Eventually(time.Second, 10*time.Millisecond, func(ptytest.Screen) error {
		if notifier.count() != 2 {
			return fmt.Errorf("actual input must rearm inactivity, got %d notifications", notifier.count())
		}
		return nil
	})
}
