//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestReconnectBannerDoesNotShiftUnchangedTopContentDown(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "reconnect-shift",
		SessionName: "reconnect-shift",
		Shell:       reconnectShell(t),
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after ctrl+l clear; got row %d col %d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt on row 1 before reconnect, got %q", row)
		}
		return nil
	})
	waitForRawIdle(t, host, 150*time.Millisecond, 2*time.Second)

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row0 := host.Screen().Row(0)
		if !strings.Contains(row0, "connection lost") && !strings.Contains(row0, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row0)
		}
		row1 := host.Screen().Row(1)
		if strings.Contains(row1, "PROMPT>") {
			return fmt.Errorf("expected no top-row shift during reconnect overlay, got row2=%q row1=%q", row1, row0)
		}
		return nil
	})
}
