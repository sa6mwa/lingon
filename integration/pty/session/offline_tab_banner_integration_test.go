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

func TestHostOfflineTabHidesReconnectBannerWhileOnlineTabStillReconnects(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "offline-banner-host",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	// Create second local tab (becomes active), then toggle it offline.
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	host.SendCtrlL()
	host.Send("o")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	// Switch back to first tab (online).
	host.SendCtrlL()
	host.Send("p")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	// Force reconnect state and verify banner appears on online tab.
	h.StopServer()
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !hasReconnectBanner(host.Screen()) {
			return fmt.Errorf("expected reconnect banner on online tab")
		}
		return nil
	})

	// Switch to the offline tab; reconnect banner must not linger there.
	host.SendCtrlL()
	host.Send("n")
	advanceTestClock(h.Clock(), 200*time.Millisecond)
	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		screen := host.Screen()
		if hasReconnectBanner(screen) {
			return fmt.Errorf("expected reconnect banner hidden on offline tab; row1=%q row2=%q", screen.Row(0), screen.Row(1))
		}
		return nil
	})

	// Switch back online tab while relay is still down: reconnect should still be active.
	host.SendCtrlL()
	host.Send("p")
	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		if !hasReconnectBanner(host.Screen()) {
			return fmt.Errorf("expected reconnect banner after switching back to online tab")
		}
		return nil
	})
}

func hasReconnectBanner(screen ptytest.Screen) bool {
	row1 := screen.Row(0)
	row2 := screen.Row(1)
	return hasReconnectText(row1) || hasReconnectText(row2)
}

func hasReconnectText(row string) bool {
	return strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting")
}
