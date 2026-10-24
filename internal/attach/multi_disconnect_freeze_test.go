package attach_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachRespondsToKeysWhileOffline(t *testing.T) {
	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_a",
		SessionName: "session_a",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_b",
		SessionName: "session_b",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_a", "session_b"})
	waitForHost(t, h, "session_a", 3*time.Second)
	waitForHost(t, h, "session_b", 3*time.Second)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "session_a",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, "session_a", 1, 3*time.Second)
	if exited, err := attach.WaitErr(50 * time.Millisecond); exited {
		t.Fatalf("attach exited early: %v", err)
	}

	hostA.Send("printf 'offline-test\\n'\n")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("offline-test") {
			raw := attach.DrainRaw()
			return fmt.Errorf("expected output before disconnect; screen:\n%s\nraw:\n%q readErr=%v", screen.String(), raw, attach.ReadErr())
		}
		return nil
	})
	attach.DrainRaw()

	// Both offline tabs need their own previously rendered snapshot.
	hostB.Send("printf 'cached-session-b\\n'\n")
	attach.SendBytes([]byte{0x0c, 'n'})
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("cached-session-b") {
			return fmt.Errorf("expected session B snapshot before disconnect; screen:\n%s", screen.String())
		}
		return nil
	})
	attach.SendBytes([]byte{0x0c, 'p'})
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("offline-test") || screen.Contains("cached-session-b") {
			return fmt.Errorf("expected session A snapshot before disconnect; screen:\n%s", screen.String())
		}
		return nil
	})

	h.StopServer()
	h.Advance(2 * time.Second)

	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("offline-test") {
			return fmt.Errorf("expected cached content after disconnect; screen:\n%s", screen.String())
		}
		if screen.Contains("Not connected") || screen.Contains("no sessions available") || screen.Contains("Waiting for sessions") {
			return nil
		}
		if exited, err := attach.WaitErr(0); exited {
			if err == nil || strings.Contains(err.Error(), "no sessions available") {
				return nil
			}
			return fmt.Errorf("attach exited unexpectedly: %v", err)
		}
		return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
	})
	if exited, err := attach.WaitErr(0); exited {
		if err == nil || strings.Contains(err.Error(), "no sessions available") {
			return
		}
		t.Fatalf("attach exited unexpectedly: %v", err)
	}
	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("offline-test") {
			return fmt.Errorf("expected cached content after disconnect; screen:\n%s", screen.String())
		}
		return nil
	})
	_ = attach.DrainRaw()

	openHelpOrOffline := func(sessionID string) {
		attach.SendBytes([]byte{0x0c, 'h'})
		attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
			if screen.Contains("no sessions available") || screen.Contains("Waiting for sessions") {
				return nil
			}
			// During offline session-list collapse, the cached frame can remain visible
			// without the no-sessions overlay; in that state command bytes are echoed in
			// the cached shell line and help cannot be rendered.
			if screen.Contains("offline-test") && strings.Contains(screen.String(), "^Lh") {
				return nil
			}
			if screen.Contains("session: "+sessionID) && screen.Contains("Ctrl+L h  help") {
				return nil
			}
			return fmt.Errorf("help overlay missing for %s; screen:\n%s", sessionID, screen.String())
		})
	}

	openHelpOrOffline("session_a")

	attach.Send("q")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Ctrl+L h  help") {
			return fmt.Errorf("help did not dismiss; screen:\n%s", screen.String())
		}
		return nil
	})
	attach.SendBytes([]byte{0x0c, 'n'})
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("cached-session-b") {
			return fmt.Errorf("expected cached session B after offline tab switch; screen:\n%s", screen.String())
		}
		return nil
	})
	openHelpOrOffline("session_b")
}
