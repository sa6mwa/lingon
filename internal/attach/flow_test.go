package attach_test

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestEndToEndHostAttachFlow(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_test",
		SessionName: "session_test",
		Shell:       "/bin/cat",
		Cols:        80,
		Rows:        24,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_test"})

	c1 := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "session_test",
		ClientID:       "client1",
		RequestControl: true,
		Cols:           80,
		Rows:           24,
	})
	c2 := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "session_test",
		ClientID:       "client2",
		RequestControl: true,
		Cols:           80,
		Rows:           24,
	})

	c2.Send("TWO\r\n")

	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("TWO") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})

	c1.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("TWO") {
			return ptytest.FormatRowDiff("attach", 0, screen.Row(0))
		}
		return nil
	})
}
