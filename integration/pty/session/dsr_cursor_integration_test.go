//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestDSRReturnsSnapshotCursorPosition(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	script := `#!/bin/bash
set -euo pipefail
if ! stty -echo -icanon min 1 time 0; then
  echo "DSR-FAIL:stty"
  exit 2
fi
trap 'stty echo icanon' EXIT

# The host owns row one for tabs; announce readiness in the terminal body.
printf '\nDSR-READY\n'
IFS= read -r start_query
[[ "$start_query" == "QUERY" ]]
printf '\033[2J\033[H'
printf '\033[6n'

resp=""
for i in {1..64}; do
  IFS= read -r -n 1 ch
  resp+="$ch"
  if [[ "$ch" == "R" ]]; then
    break
  fi
done

expected=$'\033[1;1R'
if [[ "$resp" == "$expected" ]]; then
  printf '\nDSR-OK\n'
  exit 0
fi
printf '\nDSR-BAD:'
printf '%q\n' "$resp"
exit 1
`

	dir := t.TempDir()
	path := filepath.Join(dir, "dsr_cursor.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	clk := clock.NewMock()
	master, slave := ptytest.OpenPTY(t, 80, 24)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, 80, 24, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := session.New(session.Options{
		Cols:                80,
		Rows:                24,
		Shell:               path,
		Publish:             false,
		Stdin:               slave,
		Stdout:              slave,
		DisableRaw:          true,
		DisableSignalResize: true,
		Clock:               clk,
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()
	// Wait for the child to own/configure its PTY before starting the cursor
	// query. This is a transport barrier, not a sleep through shell startup.
	sess.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		select {
		case err := <-runErr:
			t.Fatalf("runner exited before owned DSR readiness: %v; screen:\n%s", err, screen.String())
		default:
		}
		if !screen.Contains("DSR-READY") {
			return fmt.Errorf("owned DSR child is not ready; screen:\n%s", screen.String())
		}
		return nil
	})
	sess.Send("QUERY\n")

	raw := ""
	found := false
	deadline := clk.Now().Add(5 * time.Second)
	for clk.Now().Before(deadline) {
		raw += sess.DrainRaw()
		if strings.Contains(raw, "DSR-OK") {
			found = true
			break
		}
		if strings.Contains(raw, "DSR-BAD:") || strings.Contains(raw, "DSR-FAIL:") {
			t.Fatalf("unexpected DSR output; raw:\n%s", raw)
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}

	raw += sess.DrainRaw()
	if strings.Contains(raw, "DSR-BAD:") || strings.Contains(raw, "DSR-FAIL:") {
		t.Fatalf("unexpected DSR output; raw:\n%s", raw)
	}
	if !found && !strings.Contains(raw, "DSR-OK") {
		t.Fatalf("timed out waiting for DSR response; raw:\n%s", raw)
	}

	cancel()
	deadline = clk.Now().Add(2 * time.Second)
	for clk.Now().Before(deadline) {
		select {
		case err := <-runErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("runner error: %v", err)
			}
			return
		default:
			ptytest.Advance(clk, 50*time.Millisecond)
		}
	}
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("runner error: %v", err)
		}
	default:
		t.Fatalf("runner did not exit")
	}
}
