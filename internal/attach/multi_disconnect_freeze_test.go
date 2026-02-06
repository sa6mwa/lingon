package attach_test

import (
	"fmt"
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

	h.StopServer()
	h.Advance(2 * time.Second)

	attach.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("Not connected") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		if !screen.Contains("offline-test") {
			return fmt.Errorf("expected cached content after disconnect; screen:\n%s", screen.String())
		}
		return nil
	})
	_ = attach.DrainRaw()

	attach.SendBytes([]byte{0x0c, 'h'})
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("lingon controls") || !screen.Contains("session: session_a") {
			return fmt.Errorf("help overlay missing for session_a; screen:\n%s", screen.String())
		}
		return nil
	})

	attach.Send("q")
	h.Advance(200 * time.Millisecond)
	attach.SendBytes([]byte{0x0c, 'n'})
	attach.SendBytes([]byte{0x0c, 'h'})
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("lingon controls") || !screen.Contains("session: session_b") {
			return fmt.Errorf("help overlay missing for session_b; screen:\n%s", screen.String())
		}
		return nil
	})
}
