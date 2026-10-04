//go:build integration && linux

package integrationptysession_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestLocalSessionCancelWaitsForPTYReaderBeforeClose(t *testing.T) {
	script := `#!/bin/sh
printf '\nREADY\n'
while IFS= read -r line; do
  printf 'ACK:%s\n' "$line"
done
`

	dir := t.TempDir()
	path := filepath.Join(dir, "stay_ready.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	h := newHarness(t, ptytest.WithClock(clock.NewMock()))
	for i := 0; i < 20; i++ {
		host := h.StartHost(ptytest.HostOptions{
			SessionID:      fmt.Sprintf("pty-reader-shutdown-%d", i),
			Shell:          path,
			Cols:           80,
			Rows:           24,
			DisablePublish: true,
		})
		host.Eventually(3*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
			if !screen.Contains("READY") {
				return fmt.Errorf("iteration %d did not render READY: %q", i, screen.String())
			}
			return nil
		})
		nonce := fmt.Sprintf("iteration-%d", i)
		host.Send(nonce + "\n")
		host.Eventually(3*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
			if !screen.Contains("ACK:" + nonce) {
				return fmt.Errorf("iteration %d did not echo input", i)
			}
			return nil
		})
		host.Cancel()
		if exited, err := host.WaitErr(3 * time.Second); !exited || (err != nil && !errors.Is(err, context.Canceled)) {
			t.Fatalf("iteration %d did not shut down cleanly: exited=%v err=%v", i, exited, err)
		}
	}
}
