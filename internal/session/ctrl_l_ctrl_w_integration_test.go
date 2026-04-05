package session_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/control"
	"pkt.systems/lingon/internal/ptytest"
)

func TestCtrlLCtrlWCyclesWallInactivityWhileHoldingCtrl(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "ctrl-l-ctrl-w",
		SessionName: "ctrl-l-ctrl-w",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	expectStatus := func(input []byte, want string) {
		host.SendBytes(input)
		host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
			row := screen.Row(0)
			if !screen.Contains(want) {
				return fmt.Errorf("expected wall inactivity status %q, row=%q", want, row)
			}
			return nil
		})
	}

	host.SendCtrlL()
	expectStatus([]byte{control.CtrlW}, "wall inactivity 2m")
	expectStatus([]byte{control.CtrlW}, "wall inactivity 5m")
	expectStatus([]byte{control.CtrlW}, "wall inactivity 15m")
	expectStatus([]byte{control.CtrlW}, "wall inactivity off")
}
