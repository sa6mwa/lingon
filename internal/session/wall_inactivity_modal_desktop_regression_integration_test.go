package session_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/tlsmgr"
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
	hostB.Eventually(1500*time.Millisecond, 50*time.Millisecond, func(_ ptytest.Screen) error {
		if len(notifier.snapshot()) != 1 {
			return errStillVisible("waiting for source desktop notifications to remain single while idle")
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

func TestRelayBackedLocalWallInactivityDoesNotRepeatWithoutActivity(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &integrationRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-prop-no-repeat-a",
		SessionName:     "wall-prop-no-repeat-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-prop-no-repeat-b",
		SessionName: "wall-prop-no-repeat-b",
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

	if !screenContainsWithin(hostB, "wall-prop-no-repeat-a inactive", 3*time.Second) {
		t.Fatalf("expected first propagated inactivity wall modal on peer host TUI; screen:\n%s", hostB.Screen().String())
	}
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("wall-prop-no-repeat-a inactive") {
			return errStillVisible("waiting for first inactivity wall to auto-hide")
		}
		return nil
	})

	time.Sleep(1500 * time.Millisecond)
	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected exactly one desktop notification without renewed activity, got %d", len(requests))
	}
	if screenContainsWithin(hostB, "wall-prop-no-repeat-a inactive", 1200*time.Millisecond) {
		t.Fatalf("expected no repeated inactivity wall modal without renewed activity")
	}
}

func TestRelayBackedManualWallDoesNotRearmInactivity(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))
	notifier := &integrationRecordingNotifier{}

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:       "wall-manual-no-rearm-a",
		SessionName:     "wall-manual-no-rearm-a",
		Cols:            100,
		Rows:            30,
		DesktopNotifier: notifier,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-manual-no-rearm-b",
		SessionName: "wall-manual-no-rearm-b",
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

	if !screenContainsWithin(hostB, "wall-manual-no-rearm-a inactive", 3*time.Second) {
		t.Fatalf("expected first propagated inactivity wall modal on peer host TUI; screen:\n%s", hostB.Screen().String())
	}
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("wall-manual-no-rearm-a inactive") {
			return errStillVisible("waiting for first inactivity wall to auto-hide")
		}
		return nil
	})

	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), "manual hello", tlsDir, false); err != nil {
		t.Fatalf("send manual wall: %v", err)
	}
	if !screenContainsWithin(hostB, "manual hello", 1500*time.Millisecond) {
		t.Fatalf("expected manual wall modal on peer host TUI")
	}
	hostB.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("manual hello") {
			return errStillVisible("waiting for manual wall to auto-hide")
		}
		return nil
	})

	time.Sleep(1500 * time.Millisecond)
	requests := notifier.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected exactly one desktop inactivity notification after manual wall, got %d", len(requests))
	}
	if screenContainsWithin(hostB, "wall-manual-no-rearm-a inactive", 1200*time.Millisecond) {
		t.Fatalf("expected manual wall not to re-arm inactivity")
	}
}

func TestRelayBackedLocalWallInactivityClientReconnectDoesNotRearmWithoutTerminalInput(t *testing.T) {
	h := newHarness(t, ptytest.WithWallConfig(1*time.Second, []time.Duration{250 * time.Millisecond}))

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-prop-reconnect-a",
		SessionName: "wall-prop-reconnect-a",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("w")
	if !screenContainsWithin(host, "wall inactivity 250ms", 2*time.Second) {
		t.Fatalf("expected wall inactivity status banner on source tab")
	}

	waitForWallEventCount(t, h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 3*time.Second)

	conn := dialRelayClient(t, h.Endpoint(), h.AccessToken(), h.AuthFile(), "wall-prop-reconnect-a")
	_ = conn.Close(websocket.StatusNormalClosure, "bye")

	time.Sleep(1500 * time.Millisecond)

	count, events := wallEventsForTest(t, h.Endpoint(), h.AccessToken(), h.AuthFile())
	if count != 1 {
		t.Fatalf("expected reconnect without terminal input to keep wall events at 1, got %d events=%v", count, events)
	}
}

type wallEventForTest struct {
	ID              uint64 `json:"id"`
	Message         string `json:"message"`
	SourceSessionID string `json:"source_session_id"`
	Kind            uint32 `json:"kind"`
}

type wallEventsResponseForTest struct {
	Events []wallEventForTest `json:"events"`
}

func waitForWallEventCount(t *testing.T, endpoint, accessToken, authFile string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, _ := wallEventsForTest(t, endpoint, accessToken, authFile)
		if got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, events := wallEventsForTest(t, endpoint, accessToken, authFile)
	t.Fatalf("wall event count = %d, want %d; events=%v", got, want, events)
}

func wallEventsForTest(t *testing.T, endpoint, accessToken, authFile string) (int, []wallEventForTest) {
	t.Helper()
	tlsDir := filepath.Join(filepath.Dir(authFile), "tls")
	tlsPool, err := tlsmgr.LoadLocalCARoots(tlsDir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: tlsPool, MinVersion: tls.VersionTLS12}},
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"/wall/events?limit=16", nil)
	if err != nil {
		t.Fatalf("new wall events request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("wall events request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wall events status = %s", resp.Status)
	}
	var decoded wallEventsResponseForTest
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode wall events: %v", err)
	}
	return len(decoded.Events), decoded.Events
}

func dialRelayClient(t *testing.T, endpoint, accessToken, authFile, sessionID string) *websocket.Conn {
	t.Helper()
	tlsDir := filepath.Join(filepath.Dir(authFile), "tls")
	tlsPool, err := tlsmgr.LoadLocalCARoots(tlsDir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: tlsPool, MinVersion: tls.VersionTLS12}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURLForWallTest(endpoint, "/ws/client"), &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + accessToken}},
	})
	if err != nil {
		t.Fatalf("ws client dial: %v", err)
	}
	hello := &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "wall-reconnect-test",
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "client",
		}},
	}
	data, err := proto.Marshal(hello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("send client hello: %v", err)
	}
	return conn
}

func wsURLForWallTest(base, path string) string {
	ws := strings.Replace(base, "http://", "ws://", 1)
	ws = strings.Replace(ws, "https://", "wss://", 1)
	return ws + path
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
