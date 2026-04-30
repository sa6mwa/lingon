package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestLocalSessionOSCQueryDoesNotSelfSustainPublish(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	script := `#!/bin/bash
set -euo pipefail
read_until() {
  local end="$1"
  local buf=""
  for i in {1..256}; do
    if IFS= read -r -t 0.2 -n 1 ch; then
      buf+="$ch"
      if [[ "$ch" == "$end" ]]; then
        printf "%s" "$buf"
        return 0
      fi
    fi
  done
  printf "%s" "$buf"
  return 1
}

printf $'\033]10;?\a'
resp=$(read_until $'\a') || { echo "OSC10-TIMEOUT"; exit 14; }
printf $'\033]11;?\a'
resp=$(read_until $'\a') || { echo "OSC11-TIMEOUT"; exit 16; }
echo "OSC-READY"
sleep 1
echo "OSC-DONE"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "osc_once.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.New()
	var ptyBuf bytes.Buffer
	var ptyMu sync.Mutex
	ready := make(chan struct{})
	done := make(chan struct{})
	var readyOnce sync.Once
	var doneOnce sync.Once

	var frameCount atomic.Int64
	var activityCount atomic.Int64
	var renderCount atomic.Int64
	publisher := host.NewPublisher(host.PublishOptions{SessionID: "osc-repro"})
	publisher.OnFrame = func(frame *protocolpb.Frame) {
		if frame == nil {
			return
		}
		if frame.GetActivity() != nil {
			activityCount.Add(1)
			frameCount.Add(1)
		}
		if frame.GetSnapshot() != nil || frame.GetDiff() != nil || frame.GetScrollback() != nil {
			renderCount.Add(1)
			frameCount.Add(1)
		}
	}

	local := newLocalSession(ctx, localSessionOptions{
		ID:      "osc-repro",
		Name:    "osc-repro",
		Shell:   path,
		Term:    "xterm-256color",
		Cols:    80,
		Rows:    24,
		Respawn: false,
		Clock:   clk,
		OnPTYRead: func(data []byte) {
			ptyMu.Lock()
			defer ptyMu.Unlock()
			_, _ = ptyBuf.Write(data)
			if bytes.Contains(ptyBuf.Bytes(), []byte("OSC-READY")) {
				readyOnce.Do(func() { close(ready) })
			}
			if bytes.Contains(ptyBuf.Bytes(), []byte("OSC-DONE")) {
				doneOnce.Do(func() { close(done) })
			}
		},
	})
	local.SetPublisher(publisher)

	go local.Run()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("timeout waiting for OSC-READY; output=%q", out)
	}

	framesAtReady := frameCount.Load()
	readySnap := snapshotText(local.Snapshot())
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("timeout waiting for OSC-DONE; output=%q", out)
	}

	time.Sleep(1500 * time.Millisecond)
	framesAfterIdle := frameCount.Load()
	t.Logf("framesAtReady=%d framesAfterIdle=%d activity=%d render=%d containsRGB=%v", framesAtReady, framesAfterIdle, activityCount.Load(), renderCount.Load(), strings.Contains(readySnap, "rgb:"))
	if strings.Contains(readySnap, "rgb:") {
		t.Fatalf("snapshot leaked OSC query/response content: %q", readySnap)
	}
	if renderCount.Load() > 3 {
		t.Fatalf("OSC query handling produced %d render frames, want at most 3", renderCount.Load())
	}
	if framesAfterIdle > framesAtReady+3 {
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("publish frames kept growing after OSC query idle period: ready=%d afterIdle=%d output=%q", framesAtReady, framesAfterIdle, out)
	}
}

func TestLocalSessionRepeatedOSCQueriesBoundedAfterProcessIdle(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	script := `#!/bin/bash
set -euo pipefail
read_until() {
  local end="$1"
  local buf=""
  for i in {1..256}; do
    if IFS= read -r -t 0.2 -n 1 ch; then
      buf+="$ch"
      if [[ "$ch" == "$end" ]]; then
        printf "%s" "$buf"
        return 0
      fi
    fi
  done
  printf "%s" "$buf"
  return 1
}

for i in $(seq 1 200); do
  printf $'\033]10;?\a'
  read_until $'\a' >/dev/null || { echo "OSC10-TIMEOUT"; exit 14; }
  printf $'\033]11;?\a'
  read_until $'\a' >/dev/null || { echo "OSC11-TIMEOUT"; exit 16; }
done
echo "OSC-LOOP-DONE"
sleep 1
echo "OSC-LOOP-IDLE"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "osc_loop.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.New()
	var ptyBuf bytes.Buffer
	var ptyMu sync.Mutex
	loopDone := make(chan struct{})
	idle := make(chan struct{})
	var loopDoneOnce sync.Once
	var idleOnce sync.Once

	var frameCount atomic.Int64
	var activityCount atomic.Int64
	var renderCount atomic.Int64
	publisher := host.NewPublisher(host.PublishOptions{SessionID: "osc-loop"})
	publisher.OnFrame = func(frame *protocolpb.Frame) {
		if frame == nil {
			return
		}
		if frame.GetActivity() != nil {
			activityCount.Add(1)
			frameCount.Add(1)
		}
		if frame.GetSnapshot() != nil || frame.GetDiff() != nil || frame.GetScrollback() != nil {
			renderCount.Add(1)
			frameCount.Add(1)
		}
	}

	local := newLocalSession(ctx, localSessionOptions{
		ID:      "osc-loop",
		Name:    "osc-loop",
		Shell:   path,
		Term:    "xterm-256color",
		Cols:    80,
		Rows:    24,
		Respawn: false,
		Clock:   clk,
		OnPTYRead: func(data []byte) {
			ptyMu.Lock()
			defer ptyMu.Unlock()
			_, _ = ptyBuf.Write(data)
			if bytes.Contains(ptyBuf.Bytes(), []byte("OSC-LOOP-DONE")) {
				loopDoneOnce.Do(func() { close(loopDone) })
			}
			if bytes.Contains(ptyBuf.Bytes(), []byte("OSC-LOOP-IDLE")) {
				idleOnce.Do(func() { close(idle) })
			}
		},
	})
	local.SetPublisher(publisher)

	go local.Run()

	select {
	case <-loopDone:
	case <-time.After(10 * time.Second):
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("timeout waiting for OSC-LOOP-DONE; output=%q", out)
	}

	framesAtLoopDone := frameCount.Load()
	loopDoneSnap := snapshotText(local.Snapshot())
	select {
	case <-idle:
	case <-time.After(3 * time.Second):
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("timeout waiting for OSC-LOOP-IDLE; output=%q", out)
	}
	time.Sleep(1500 * time.Millisecond)
	framesAfterIdle := frameCount.Load()
	t.Logf("framesAtLoopDone=%d framesAfterIdle=%d activity=%d render=%d containsRGB=%v", framesAtLoopDone, framesAfterIdle, activityCount.Load(), renderCount.Load(), strings.Contains(loopDoneSnap, "rgb:"))
	if strings.Contains(loopDoneSnap, "rgb:") {
		t.Fatalf("snapshot leaked repeated OSC query/response content: %q", loopDoneSnap)
	}
	if renderCount.Load() > 4 {
		t.Fatalf("repeated OSC query handling produced %d render frames, want at most 4", renderCount.Load())
	}
	if framesAfterIdle > framesAtLoopDone+3 {
		ptyMu.Lock()
		out := ptyBuf.String()
		ptyMu.Unlock()
		t.Fatalf("publish frames kept growing after repeated OSC query process went idle: loopDone=%d afterIdle=%d output=%q", framesAtLoopDone, framesAfterIdle, out)
	}
}

func snapshotText(snap *protocolpb.Snapshot) string {
	if snap == nil {
		return ""
	}
	var b strings.Builder
	for _, r := range snap.Runes {
		b.WriteRune(rune(r))
	}
	return b.String()
}
