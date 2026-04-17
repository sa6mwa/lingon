package session_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func reconnectNoisyTopRowShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "reconnect-noisy-top-row.sh")
	const script = `#!/usr/bin/env bash
printf '170223 [server exited unexpectedly]'
while IFS= read -r _line; do
  :
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write reconnect noisy top-row shell: %v", err)
	}
	return scriptPath
}

func TestReconnectBannerOwnsRowWithoutUnderlyingTopRowBleed(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "row1-own-reconnect",
		SessionName: "row1-own-reconnect",
		Shell:       reconnectNoisyTopRowShell(t),
		Cols:        120,
		Rows:        20,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		if strings.Contains(row, "170223") || strings.Contains(row, "server exited unexpectedly") {
			return fmt.Errorf("expected reconnect banner to own row 1 without base-row bleed, got %q", row)
		}
		return nil
	})
}
