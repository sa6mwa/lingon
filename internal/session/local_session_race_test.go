package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestLocalSessionCancelWaitsForPTYReaderBeforeClose(t *testing.T) {
	script := `#!/bin/sh
printf READY
while true; do
  sleep 1
done
`

	dir := t.TempDir()
	path := filepath.Join(dir, "stay_ready.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		var output bytes.Buffer
		var outputMu sync.Mutex
		ready := make(chan struct{})
		var readyOnce sync.Once

		local := newLocalSession(ctx, localSessionOptions{
			ID:      "pty-reader-shutdown",
			Name:    "pty-reader-shutdown",
			Shell:   path,
			Term:    "xterm-256color",
			Cols:    80,
			Rows:    24,
			Respawn: false,
			Clock:   clock.New(),
			OnPTYRead: func(data []byte) {
				outputMu.Lock()
				defer outputMu.Unlock()
				_, _ = output.Write(data)
				if bytes.Contains(output.Bytes(), []byte("READY")) {
					readyOnce.Do(func() { close(ready) })
				}
			},
		})

		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = local.runOnce(ctx)
		}()

		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
			outputMu.Lock()
			out := output.String()
			outputMu.Unlock()
			t.Fatalf("iteration %d timed out waiting for READY; output=%q", i, out)
		}

		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d timed out waiting for local session shutdown", i)
		}
	}
}
