package session_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

type integrationRecordingNotifier struct {
	mu       sync.Mutex
	requests []desktopnotify.Request
}

func (n *integrationRecordingNotifier) Notify(_ context.Context, req desktopnotify.Request) error {
	n.mu.Lock()
	n.requests = append(n.requests, req)
	n.mu.Unlock()
	return nil
}

func (n *integrationRecordingNotifier) snapshot() []desktopnotify.Request {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]desktopnotify.Request, len(n.requests))
	copy(out, n.requests)
	return out
}

func TestLocalWallInactivityShowsModalOnOtherLocalTabAndDesktopNotification(t *testing.T) {
	clk := clock.NewMock()
	h := newHarness(t, ptytest.WithClock(clk))
	notifier := &integrationRecordingNotifier{}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-local-a",
		SessionName:     "wall-local-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	firstMark := "DUAL_FOCUS_A"
	host.Send("echo " + firstMark + "\n")
	if !screenContainsWithin(host, firstMark, 2*time.Second) {
		t.Fatalf("expected first-session marker before creating sibling")
	}

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	secondMark := "DUAL_FOCUS_B"
	host.Send("echo " + secondMark + "\n")
	if !screenContainsWithin(host, secondMark, 2*time.Second) {
		t.Fatalf("expected second-session marker after creating sibling")
	}

	host.SendCtrlL()
	host.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 2m", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	host.SendCtrlL()
	host.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	advanceTestClock(h.Clock(), 2*time.Minute+2*time.Second)

	if !screenContainsWithin(host, "wall-local-a inactive", 2*time.Second) {
		t.Fatalf("expected inactivity wall modal on other local tab")
	}

	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(requests))
	}
	if requests[0].Title != "wall-local-a" {
		t.Fatalf("desktop notification Title = %q, want %q", requests[0].Title, "wall-local-a")
	}
	if requests[0].Body != "inactive" {
		t.Fatalf("desktop notification Body = %q, want %q", requests[0].Body, "inactive")
	}
}

func TestLocalWallInactivitySkipsSelfModalOnFocusedSourceTab(t *testing.T) {
	clk := clock.NewMock()
	h := newHarness(t, ptytest.WithClock(clk))
	notifier := &integrationRecordingNotifier{}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-self-a",
		SessionName:     "wall-self-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 2m", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	advanceTestClock(h.Clock(), 2*time.Minute+2*time.Second)

	if screenContainsWithin(host, "wall-self-a inactive", 500*time.Millisecond) {
		t.Fatalf("expected no inactivity wall modal on focused source tab")
	}

	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(requests))
	}
	if requests[0].Title != "wall-self-a" {
		t.Fatalf("desktop notification Title = %q, want %q", requests[0].Title, "wall-self-a")
	}
	if requests[0].Body != "inactive" {
		t.Fatalf("desktop notification Body = %q, want %q", requests[0].Body, "inactive")
	}
}

func TestRelayWallInactivitySkipsSelfModalOnFocusedSourceTab(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(5*time.Second, []time.Duration{time.Second}))
	notifier := &integrationRecordingNotifier{}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-relay-self-a",
		SessionName:     "wall-relay-self-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 1s", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	host.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		requests := notifier.snapshot()
		if len(requests) != 1 {
			return errStillVisible("waiting for desktop notification")
		}
		return nil
	})

	if screenContainsWithin(host, "wall-relay-self-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected no inactivity wall modal on focused relay-backed source tab")
	}

	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(requests))
	}
	if requests[0].Title != "wall-relay-self-a" {
		t.Fatalf("desktop notification Title = %q, want %q", requests[0].Title, "wall-relay-self-a")
	}
	if requests[0].Body != "inactive" {
		t.Fatalf("desktop notification Body = %q, want %q", requests[0].Body, "inactive")
	}
}

func TestInactivityShapedRelayWallDoesNotSuppressFocusedTabWithoutExplicitKind(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-relay-shaped-a",
		SessionName: "wall-relay-shaped-a",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), "wall-relay-shaped-a inactive", tlsDir, false); err != nil {
		t.Fatalf("send wall: %v", err)
	}

	if !screenContainsWithin(host, "wall-relay-shaped-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected plain relay wall to remain visible without explicit inactivity kind")
	}
}

func TestInactivityShapedRelayWallDoesNotSuppressFocusedTabWithSiblingLocalPTYWithoutExplicitKind(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-relay-shaped-sibling-a",
		SessionName: "wall-relay-shaped-sibling-a",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	firstMark := "DUAL_FOCUS_A"
	host.Send("echo " + firstMark + "\n")
	if !screenContainsWithin(host, firstMark, 2*time.Second) {
		t.Fatalf("expected first-session marker before creating sibling")
	}

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	secondMark := "DUAL_FOCUS_B"
	host.Send("echo " + secondMark + "\n")
	if !screenContainsWithin(host, secondMark, 2*time.Second) {
		t.Fatalf("expected second-session marker after creating sibling")
	}

	host.SendCtrlL()
	host.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), "wall-relay-shaped-sibling-a inactive", tlsDir, false); err != nil {
		t.Fatalf("send wall: %v", err)
	}

	if !screenContainsWithin(host, "wall-relay-shaped-sibling-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected plain relay wall to remain visible without explicit inactivity kind")
	}
}

func TestTwoEnabledLocalPTYsSuppressFocusedSelfModalAndAvoidDuplicateNotifications(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(5*time.Second, []time.Duration{time.Second}))
	notifier := &integrationRecordingNotifier{}

	host := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-dual-a",
		SessionName:     "wall-dual-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	firstMark := "DUAL_FOCUS_A"
	host.Send("echo " + firstMark + "\n")
	if !screenContainsWithin(host, firstMark, 2*time.Second) {
		t.Fatalf("expected first-session marker before creating sibling")
	}

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	secondMark := "DUAL_FOCUS_B"
	host.Send("echo " + secondMark + "\n")
	if !screenContainsWithin(host, secondMark, 2*time.Second) {
		t.Fatalf("expected second-session marker after creating sibling")
	}

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 1s", 2*time.Second) {
		t.Fatalf("expected wall inactivity banner for first session")
	}

	host.SendCtrlL()
	host.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 1s", 2*time.Second) {
		t.Fatalf("expected wall inactivity banner for second session")
	}

	host.SendCtrlL()
	host.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	if !switchToToken(host, firstMark, 3, 500*time.Millisecond) {
		t.Fatalf("expected focus to return to wall-dual-a before inactivity timers")
	}

	host.Eventually(4*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		requests := notifier.snapshot()
		if len(requests) != 2 {
			return errStillVisible("waiting for both desktop notifications")
		}
		return nil
	})

	if screenContainsWithin(host, "wall-dual-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected no focused self-modal for wall-dual-a; screen:\n%s", host.Screen().String())
	}
	if !screenContainsWithin(host, "wall-dual-a-2 inactive", 1500*time.Millisecond) {
		t.Fatalf("expected sibling inactivity modal for wall-dual-a-2; screen:\n%s", host.Screen().String())
	}

	requests := notifier.snapshot()
	if len(requests) != 2 {
		t.Fatalf("expected exactly two desktop notifications, got %d", len(requests))
	}
	got := map[string]int{}
	for _, req := range requests {
		got[req.Title]++
		if req.Body != "inactive" {
			t.Fatalf("desktop notification Body = %q, want %q", req.Body, "inactive")
		}
	}
	if got["wall-dual-a"] != 1 {
		t.Fatalf("expected one desktop notification for wall-dual-a, got %d", got["wall-dual-a"])
	}
	if got["wall-dual-a-2"] != 1 {
		t.Fatalf("expected one desktop notification for wall-dual-a-2, got %d", got["wall-dual-a-2"])
	}
}

func TestRelayBackedLocalWallInactivityPropagatesModalToOtherHostTUI(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(5*time.Second, []time.Duration{time.Second}))
	notifier := &integrationRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-prop-a",
		SessionName:     "wall-prop-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-prop-b",
		SessionName: "wall-prop-b",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 1s", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		requests := notifier.snapshot()
		if len(requests) != 1 {
			return errStillVisible("waiting for desktop notification")
		}
		return nil
	})

	if screenContainsWithin(hostA, "wall-prop-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected no inactivity wall modal on focused source tab")
	}
	if !screenContainsWithin(hostB, "wall-prop-a inactive", 3*time.Second) {
		t.Fatalf("expected propagated inactivity wall modal on peer host TUI; screen:\n%s", hostB.Screen().String())
	}

	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(requests))
	}
	if requests[0].Title != "wall-prop-a" {
		t.Fatalf("desktop notification Title = %q, want %q", requests[0].Title, "wall-prop-a")
	}
	if requests[0].Body != "inactive" {
		t.Fatalf("desktop notification Body = %q, want %q", requests[0].Body, "inactive")
	}
}

func TestRelayBackedLocalWallInactivityPropagatesAfterSecondIdlePeriod(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &integrationRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-prop-repeat-a",
		SessionName:     "wall-prop-repeat-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-prop-repeat-b",
		SessionName: "wall-prop-repeat-b",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	if !screenContainsWithin(hostB, "wall-prop-repeat-a inactive", 3*time.Second) {
		t.Fatalf("expected first propagated inactivity wall modal on peer host TUI; screen:\n%s", hostB.Screen().String())
	}
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("wall-prop-repeat-a inactive") {
			return errStillVisible("waiting for first inactivity wall to auto-hide")
		}
		return nil
	})

	hostA.Send("echo SECOND_IDLE_ARM\n")
	if !screenContainsWithin(hostA, "SECOND_IDLE_ARM", 2*time.Second) {
		t.Fatalf("expected activity marker before second idle period")
	}
	if !screenContainsWithin(hostB, "wall-prop-repeat-a inactive", 3*time.Second) {
		t.Fatalf("expected second propagated inactivity wall modal on peer host TUI; screen:\n%s", hostB.Screen().String())
	}

	requests := notifier.snapshot()
	if len(requests) < 2 {
		t.Fatalf("expected repeated desktop notifications on source host, got %d", len(requests))
	}
}

func TestRelayBackedLocalWallInactivityPropagatesModalToAttachTUI(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(5*time.Second, []time.Duration{time.Second}))
	notifier := &integrationRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-prop-attach-a",
		SessionName:     "wall-prop-attach-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-prop-attach-b",
		SessionName: "wall-prop-attach-b",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "wall-prop-attach-b",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	hostBMark := "WALL_PROP_ATTACH_B_READY"
	hostB.Send("echo " + hostBMark + "\n")
	if !screenContainsWithin(attachSess, hostBMark, 3*time.Second) {
		t.Fatalf("expected attach TUI to be ready on peer session before inactivity trigger; screen:\n%s", attachSess.Screen().String())
	}

	hostA.SendCtrlL()
	hostA.Send("w")
	if !screenContainsWithin(hostA, "wall inactivity 1s", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	hostA.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		requests := notifier.snapshot()
		if len(requests) != 1 {
			return errStillVisible("waiting for desktop notification")
		}
		return nil
	})

	if screenContainsWithin(hostA, "wall-prop-attach-a inactive", 1500*time.Millisecond) {
		t.Fatalf("expected no inactivity wall modal on focused source tab")
	}
	if !screenContainsWithin(attachSess, "wall-prop-attach-a inactive", 3*time.Second) {
		t.Fatalf("expected propagated inactivity wall modal on attach TUI; screen:\n%s", attachSess.Screen().String())
	}

	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one desktop notification, got %d", len(requests))
	}
	if requests[0].Title != "wall-prop-attach-a" {
		t.Fatalf("desktop notification Title = %q, want %q", requests[0].Title, "wall-prop-attach-a")
	}
	if requests[0].Body != "inactive" {
		t.Fatalf("desktop notification Body = %q, want %q", requests[0].Body, "inactive")
	}
}
