package attach_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

func TestMultiAttachStartupHidesTabsWhenCursorOwnsTopRow(t *testing.T) {
	shellPath := writeAttachIdleShell(t)
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-startup-top-row",
		SessionName: "attach-startup-top-row",
		Shell:       shellPath,
		Cols:        120,
		Rows:        30,
		DisableRaw:  true,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-startup-top-row"})

	for i := 0; i < 8; i++ {
		attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
			SessionID: "attach-startup-top-row",
			Cols:      120,
			Rows:      30,
		})
		t.Cleanup(attachSess.Cancel)

		attachSess.Eventually(5*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
			cur := attachSess.Cursor()
			if cur.Row != 1 {
				return fmt.Errorf("expected cursor on row 1 at startup; got row %d col %d", cur.Row, cur.Col)
			}
			row := screen.Row(0)
			if strings.Contains(row, "attach-startup-top-row") {
				return fmt.Errorf("expected tab bar hidden while cursor owns row 1 at startup; row=%q", row)
			}
			return nil
		})

		attachSess.Cancel()
		_, _ = attachSess.WaitErr(2 * time.Second)
	}
}

func writeAttachIdleShell(t *testing.T) string {
	t.Helper()
	const script = "#!/bin/sh\nwhile :; do sleep 1; done\n"
	path := filepath.Join(testutil.TempDir(t), "attach-idle.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write attach idle shell: %v", err)
	}
	return path
}
