//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

func TestWallModalPropagatesToActiveOfflineLocalTabWhenAnotherLocalTabOnline(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-offline-local",
		SessionName: "wall-offline-local",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	// Create second local tab (becomes active) and make it offline.
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	host.SendCtrlL()
	host.Send("o")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	// Keep the offline tab active and send wall message.
	wallMsg := "WALL_OFFLINE_LOCAL_TAB"
	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), wallMsg, tlsDir, false); err != nil {
		t.Fatalf("send wall: %v", err)
	}
	if !screenContainsWithin(host, wallMsg, 3*time.Second) {
		t.Fatalf("expected wall modal while active tab is offline local tab")
	}
}
