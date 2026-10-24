package attach_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/testclock"
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
	connected := make(chan struct{})
	var once sync.Once
	onPublishStatus := opts.OnPublishStatus
	opts.OnPublishStatus = func(status session.PublishStatus) {
		if onPublishStatus != nil {
			onPublishStatus(status)
		}
		if status.Kind == session.PublishStatusConnected {
			once.Do(func() { close(connected) })
		}
	}
	host := h.StartHost(opts)
	t.Cleanup(host.Cancel)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-connected:
	case <-ctx.Done():
		t.Fatal("owned inactivity host did not connect:", ctx.Err())
	}
	waitInactivityScreen(t, host, "INACTIVITY-SHELL-READY")
	return host
}

func waitInactivityScreen(t *testing.T, sess *ptytest.PTYSession, marker string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	before := sess.Clock().Now()
	if err := sess.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains(marker) }); err != nil {
		t.Fatalf("waiting for %q: %v; screen:\n%s", marker, err, sess.Screen().String())
	}
	if !sess.Clock().Now().Equal(before) {
		t.Fatal("waiting for rendered output advanced the inactivity clock")
	}
}

func advanceInactivityClocks(hostClock, wallClock testclock.Mock, duration time.Duration) {
	wallClock.Add(duration)
	hostClock.Add(duration)
}

func TestAttachConnectDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	wallClock, hostClock := testclock.New(), testclock.New()
	h := newHarness(t, ptytest.WithClock(hostClock), ptytest.WithWallClock(wallClock), ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &attachRecordingNotifier{}

	host := startInactivityHost(t, h, ptytest.HostOptions{
		SessionID:       "attach-wall-connect",
		SessionName:     "attach-wall-connect",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})

	host.Send("echo ATTACH_WALL_CONNECT_READY\n")
	waitInactivityScreen(t, host, "ATTACH_WALL_CONNECT_READY")

	host.SendCtrlL()
	host.Send("w")
	waitInactivityScreen(t, host, "wall inactivity 250ms")
	advanceInactivityClocks(hostClock, wallClock, time.Second)
	if got := notifier.count(); got != 1 {
		t.Fatalf("first inactivity produced %d notifications, want 1", got)
	}

	attachSess := h.StartAttach(ptytest.AttachOptions{
		SessionID: "attach-wall-connect",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	waitInactivityScreen(t, attachSess, "ATTACH_WALL_CONNECT_READY")

	advanceInactivityClocks(hostClock, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected attach connect/replay to avoid rearming inactivity, got %d notifications", got)
	}
	host.Send("echo ATTACH_INPUT_REARMS\n")
	waitInactivityScreen(t, host, "ATTACH_INPUT_REARMS")
	advanceInactivityClocks(hostClock, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 2 {
		t.Fatalf("actual input must rearm inactivity, got %d notifications", got)
	}
}

func TestMultiAttachTabSwitchDoesNotRearmWallInactivityWithoutTerminalInput(t *testing.T) {
	wallClock, hostClock := testclock.New(), testclock.New()
	h := newHarness(t, ptytest.WithClock(hostClock), ptytest.WithWallClock(wallClock), ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
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

	hostA.Send("echo ATTACH_WALL_SWITCH_A\n")
	hostB.Send("echo ATTACH_WALL_SWITCH_B\n")
	waitInactivityScreen(t, hostA, "ATTACH_WALL_SWITCH_A")
	waitInactivityScreen(t, hostB, "ATTACH_WALL_SWITCH_B")

	hostA.SendCtrlL()
	hostA.Send("w")
	waitInactivityScreen(t, hostA, "wall inactivity 250ms")
	advanceInactivityClocks(hostClock, wallClock, time.Second)
	if got := notifier.count(); got != 1 {
		t.Fatalf("first inactivity produced %d notifications, want 1", got)
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-wall-switch-b",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	waitInactivityScreen(t, attachSess, "ATTACH_WALL_SWITCH_B")

	attachSess.SendCtrlL()
	attachSess.Send("n")
	waitInactivityScreen(t, attachSess, "ATTACH_WALL_SWITCH_A")

	advanceInactivityClocks(hostClock, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 1 {
		t.Fatalf("expected multi-attach tab switch to avoid rearming inactivity, got %d notifications", got)
	}
	hostA.Send("echo TAB_INPUT_REARMS\n")
	waitInactivityScreen(t, hostA, "TAB_INPUT_REARMS")
	advanceInactivityClocks(hostClock, wallClock, 1500*time.Millisecond)
	if got := notifier.count(); got != 2 {
		t.Fatalf("actual input must rearm inactivity, got %d notifications", got)
	}
}
