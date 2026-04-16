package attach_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayclient"
)

const longWallModalMessage = "Fixed tarball root layout. make release passed, release_tarball_sdk_test passed, and the packaged archives now root under liblockdc-<version>-<target>/ with no ./ entries."

func TestAttachWallModalShowsWrappedLongMessage(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-wall-long",
		SessionName: "attach-wall-long",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-wall-long"})

	host.Send("echo ATTACH_WALL_LONG_READY\n")
	if !screenContainsWithin(host, "ATTACH_WALL_LONG_READY", 2*time.Second) {
		t.Fatalf("expected host session marker before attach connects")
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-wall-long",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attachSess.Cancel)

	if !screenContainsWithin(attachSess, "ATTACH_WALL_LONG_READY", 3*time.Second) {
		t.Fatalf("expected attach session content before wall modal")
	}

	tlsDir := filepath.Join(filepath.Dir(h.AuthFile()), "tls")
	if _, err := relayclient.SendWall(context.Background(), h.Endpoint(), h.AccessToken(), longWallModalMessage, tlsDir, false); err != nil {
		t.Fatalf("send wall: %v", err)
	}

	if !screenContainsWithin(attachSess, "Fixed tarball root layout.", 3*time.Second) {
		t.Fatalf("expected first wrapped wall segment in attach modal")
	}
	if !screenContainsWithin(attachSess, "release_tarball_sdk_test passed,", 3*time.Second) {
		t.Fatalf("expected second wrapped wall segment in attach modal")
	}
	if !screenContainsWithin(attachSess, "archives now root under", 3*time.Second) {
		t.Fatalf("expected third wrapped wall segment in attach modal; screen:\n%s", attachSess.Screen().String())
	}
}
