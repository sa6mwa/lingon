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

func TestCtrlLCreateSession(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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

	waitForSessionCount(t, runner, 1, 2*time.Second)

	_, _ = inW.Write([]byte{0x0c, 'c'})
	waitForSessionCount(t, runner, 2, 2*time.Second)

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func TestCtrlLCreateSessionActivatesNewTabInTmuxTerm(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.NewMock()
	runner := New(Options{
		Shell:      scriptPath,
		Term:       "tmux-256color",
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

	waitForSessionCount(t, runner, 1, 2*time.Second)
	_, _ = inW.Write([]byte{0x0c, 'c'})
	waitForSessionCount(t, runner, 2, 2*time.Second)

	runner.localMu.RLock()
	order := append([]string(nil), runner.localOrder...)
	runner.localMu.RUnlock()
	if len(order) < 2 {
		t.Fatalf("expected two local sessions")
	}
	wantActive := order[1]

	waitUntilActive := func(timeout time.Duration) bool {
		deadline := runner.clock.Now().Add(timeout)
		for runner.clock.Now().Before(deadline) {
			activeID, activeLocal := runner.activeSession()
			if activeLocal && activeID == wantActive {
				return true
			}
			advanceClock(runner.clock, 20*time.Millisecond)
		}
		return false
	}
	if !waitUntilActive(2 * time.Second) {
		activeID, activeLocal := runner.activeSession()
		t.Fatalf("expected active session to switch to %q (local), got id=%q local=%v", wantActive, activeID, activeLocal)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func TestCtrlLToggleRespawn(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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

	waitForSessionCount(t, runner, 1, 2*time.Second)

	local := runner.localSession(runner.opts.SessionID)
	if local == nil {
		t.Fatalf("missing initial local session")
	}
	if local.RespawnEnabled() {
		t.Fatalf("expected respawn disabled initially")
	}

	_, _ = inW.Write([]byte{0x0c, 'r'})
	waitForRespawnState(t, runner.clock, local, true, 2*time.Second)

	_, _ = inW.Write([]byte{0x0c, 'r'})
	waitForRespawnState(t, runner.clock, local, false, 2*time.Second)

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func TestCtrlLToggleOffline(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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

	waitForSessionCount(t, runner, 1, 2*time.Second)

	local := runner.localSession(runner.opts.SessionID)
	if local == nil {
		t.Fatalf("missing initial local session")
	}
	if local.Offline() {
		t.Fatalf("expected offline disabled initially")
	}

	_, _ = inW.Write([]byte{0x0c, 'o'})
	waitForOfflineState(t, runner.clock, local, true, 2*time.Second)

	_, _ = inW.Write([]byte{0x0c, 'o'})
	waitForOfflineState(t, runner.clock, local, false, 2*time.Second)

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func TestCtrlLCreateSessionInheritsOfflineStartFlag(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.NewMock()
	runner := New(Options{
		Shell:      scriptPath,
		Cols:       80,
		Rows:       24,
		Stdin:      inR,
		Stdout:     outW,
		DisableRaw: true,
		Clock:      clk,
		Offline:    true,
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	waitForSessionCount(t, runner, 1, 2*time.Second)

	initial := runner.localSession(runner.opts.SessionID)
	if initial == nil {
		t.Fatalf("missing initial local session")
	}
	if !initial.Offline() {
		t.Fatalf("expected initial local session offline")
	}

	_, _ = inW.Write([]byte{0x0c, 'c'})
	waitForSessionCount(t, runner, 2, 2*time.Second)

	runner.localMu.RLock()
	order := append([]string(nil), runner.localOrder...)
	runner.localMu.RUnlock()
	if len(order) < 2 {
		t.Fatalf("expected at least 2 local sessions")
	}
	created := runner.localSession(order[1])
	if created == nil {
		t.Fatalf("missing created local session")
	}
	if !created.Offline() {
		t.Fatalf("expected created local session offline")
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func TestCtrlLToggleOfflineUpdatesTabMutedState(t *testing.T) {
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
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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

	waitForSessionCount(t, runner, 1, 2*time.Second)
	waitForTabMutedState(t, runner.clock, runner, false, 2*time.Second)

	_, _ = inW.Write([]byte{0x0c, 'o'})
	waitForTabMutedState(t, runner.clock, runner, true, 2*time.Second)

	_, _ = inW.Write([]byte{0x0c, 'o'})
	waitForTabMutedState(t, runner.clock, runner, false, 2*time.Second)

	cancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("session did not exit")
	}
}

func waitForSessionCount(t *testing.T, runner *Runner, want int, timeout time.Duration) {
	t.Helper()
	clk := runner.clock
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		runner.localMu.RLock()
		count := len(runner.localSessions)
		runner.localMu.RUnlock()
		if count == want {
			return
		}
		advanceClock(clk, 20*time.Millisecond)
	}
	t.Fatalf("expected %d local sessions", want)
}

func waitForRespawnState(t *testing.T, clk clock.Clock, session *localSession, want bool, timeout time.Duration) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if session.RespawnEnabled() == want {
			return
		}
		advanceClock(clk, 20*time.Millisecond)
	}
	t.Fatalf("expected respawn=%v", want)
}

func waitForOfflineState(t *testing.T, clk clock.Clock, session *localSession, want bool, timeout time.Duration) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if session.Offline() == want {
			return
		}
		advanceClock(clk, 20*time.Millisecond)
	}
	t.Fatalf("expected offline=%v", want)
}

func waitForTabMutedState(t *testing.T, clk clock.Clock, runner *Runner, want bool, timeout time.Duration) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if runner.compositor != nil {
			state := runner.compositor.State()
			if len(state.Tabs) > 0 && state.Tabs[state.ActiveTab].Muted == want {
				return
			}
		}
		advanceClock(clk, 20*time.Millisecond)
	}
	t.Fatalf("expected active tab muted=%v", want)
}
