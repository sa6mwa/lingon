package attach_test

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachExitsWhenHostDisconnects(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_disconnect",
		SessionName: "session_disconnect",
		Shell:       "/bin/cat",
		Cols:        80,
		Rows:        24,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_disconnect"})

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "session_disconnect",
		ClientID:       "attach1",
		RequestControl: true,
		Cols:           80,
		Rows:           24,
	})

	attach.Send("PING\n")
	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("PING") {
			return ptytest.FormatRowDiff("host", 0, screen.Row(0))
		}
		return nil
	})

	host.Cancel()
	_, _ = host.WaitErr(2 * time.Second)

	deadline := ptytest.Now(h.Clock()).Add(5 * time.Second)
	var (
		ok  bool
		err error
	)
	for ptytest.Now(h.Clock()).Before(deadline) {
		h.Advance(500 * time.Millisecond)
		ok, err = attach.WaitErr(25 * time.Millisecond)
		if ok {
			break
		}
	}
	if !ok {
		t.Fatalf("attach did not exit after host disconnect")
	}
	if err == nil || !strings.Contains(err.Error(), "host disconnected") {
		t.Fatalf("unexpected attach error: %v", err)
	}
}
