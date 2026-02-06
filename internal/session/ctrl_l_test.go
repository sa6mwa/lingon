package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/testutil"
)

func TestCtrlLQuitStopsSession(t *testing.T) {
	runner, inW, runErr, cleanup := newCtrlLTestRunner(t)
	t.Cleanup(cleanup)

	advanceClock(runner.clock, 100*time.Millisecond)
	_, _ = inW.Write([]byte{0x0c, 'Q'})

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("ctrl-l Q did not stop session")
	}
}

func TestCtrlLLowercaseQDoesNotQuitSession(t *testing.T) {
	_, inW, runErr, cleanup := newCtrlLTestRunner(t)
	t.Cleanup(cleanup)

	_, _ = inW.Write([]byte{0x0c, 'q'})

	select {
	case err := <-runErr:
		t.Fatalf("ctrl-l q should not quit session, got run error: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

func newCtrlLTestRunner(t *testing.T) (*Runner, *os.File, <-chan error, func()) {
	t.Helper()
	scriptPath := filepath.Join(testutil.TempDir(t), "sleep.sh")
	script := "#!/bin/sh\n" +
		"trap '' TERM\n" +
		"sleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		t.Fatalf("stdout pipe: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	clk := clock.NewMock()
	runner := New(Options{
		Shell:      scriptPath,
		Cols:       80,
		Rows:       24,
		Stdin:      inR,
		Stdout:     outW,
		DisableRaw: true,
		Clock:      clk,
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	cleanup := func() {
		cancel()
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
	}

	return runner, inW, runErr, cleanup
}
