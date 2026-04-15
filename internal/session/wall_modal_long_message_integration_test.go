package session_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

const longWallModalMessage = "Fixed tarball root layout. make release passed, release_tarball_sdk_test passed, and the packaged archives now root under liblockdc-<version>-<target>/ with no ./ entries."

func TestHostWallModalShowsWrappedLongMessage(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "wall-long-host",
		SessionName: "wall-long-host",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)
	host.SendCtrlL()
	host.Send("o")
	advanceTestClock(h.Clock(), 200*time.Millisecond)

	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), longWallModalMessage, tlsDir, false); err != nil {
		t.Fatalf("send wall: %v", err)
	}

	if !screenContainsWithin(host, "Fixed tarball root layout.", 3*time.Second) {
		t.Fatalf("expected first wrapped wall segment in host modal")
	}
	if !screenContainsWithin(host, "release_tarball_sdk_test passed,", 3*time.Second) {
		t.Fatalf("expected second wrapped wall segment in host modal")
	}
	if !screenContainsWithin(host, "archives now root under", 3*time.Second) {
		t.Fatalf("expected third wrapped wall segment in host modal; screen:\n%s", host.Screen().String())
	}
}
