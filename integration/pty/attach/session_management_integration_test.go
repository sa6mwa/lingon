//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/sessionmanage"
)

func cleanupManagedTerminals(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if records, err := headless.NewStore(dir).Reconcile(); err == nil {
			for _, rec := range records {
				_ = headless.DetachSession(context.Background(), dir, rec.SessionID)
			}
		}
	})
}

func TestSessionManagementLocalCLI(t *testing.T) {
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	bin := buildLingonAttachBinary(t)
	source := "management-source"
	cmd := exec.Command(bin, "-x", "--offline", "--session", source, "--shell", fixedPromptEmitRowsBash(t))
	cmd.Env = testAttachCLIEnv(home)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start: %v %s", err, output)
	}
	original, _ := headless.SocketPath(cfg, source)
	terminal := startLingonAttachHeadlessCLI(t, home, source, 100, 24, "--log-file", filepath.Join(cfg, "management-test.log"))
	t.Cleanup(terminal.Cancel)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		exited, runErr := terminal.WaitErr(0)
		raw := terminal.DrainRaw()
		if len(raw) > 32*1024 {
			raw = raw[len(raw)-32*1024:]
		}
		t.Logf("attach exited=%t err=%v raw tail=%q", exited, runErr, raw)
		if state, err := headless.NewStore(cfg).Load(); err == nil {
			t.Logf("local state: %+v", state)
		}
		if data, err := os.ReadFile(filepath.Join(cfg, "management-test.log")); err == nil {
			t.Logf("client log: %s", data)
		}
	})
	waitForPromptVisible(t, terminal, 8*time.Second)
	terminal.Send("\x0ceLocal build 🦊\r")
	terminal.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		state, err := headless.NewStore(cfg).Load()
		if err != nil {
			return err
		}
		rec := state.Sessions[source]
		if rec.Name != "Local build 🦊" || rec.SocketPath != original {
			return fmt.Errorf("rename metadata: %+v", rec)
		}
		return nil
	})
	terminal.DrainRaw()
	terminal.Send("\x0cc")
	var child string
	terminal.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		records, err := headless.NewStore(cfg).Reconcile()
		if err != nil {
			return err
		}
		if len(records) != 2 {
			return fmt.Errorf("have %d terminals", len(records))
		}
		for _, record := range records {
			if record.SessionID != source {
				child = record.SessionID
			}
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("new terminal not ready: %s", screen.String())
		}
		return nil
	})
	if raw := terminal.DrainRaw(); strings.Contains(raw, "\x1b[2J") {
		t.Fatal("creation cleared the whole screen")
	}
	terminal.Send("echo SELECTED_CHILD_OK\r")
	terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SELECTED_CHILD_OK") {
			return fmt.Errorf("child input not rendered")
		}
		return nil
	})
	// A fresh connection to the new socket proves creation selected the child, not its parent.
	observer := startLingonAttachHeadlessCLI(t, home, child, 100, 24)
	t.Cleanup(observer.Cancel)
	observer.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SELECTED_CHILD_OK") {
			return fmt.Errorf("created shell did not receive input: %s", screen.String())
		}
		return nil
	})
	if err := headless.DetachSession(context.Background(), cfg, source); err != nil {
		t.Fatal(err)
	}
	terminal.Cancel()
	observer.Send("echo SURVIVES_PARENT_OK\r")
	observer.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SURVIVES_PARENT_OK") {
			return fmt.Errorf("child did not survive parent exit")
		}
		return nil
	})
}

func TestSessionManagementOfflineCredentialsStayOfflineUntilExplicitToggle(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.NewMock()))
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	source := "offline-management-source"
	cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--session", source, "--shell", fixedPromptEmitRowsBash(t), "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
	cmd.Env = testAttachCLIEnv(home)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start offline authenticated host: %v %s", err, output)
	}
	terminal := startLingonAttachHeadlessCLI(t, home, source, 100, 24)
	t.Cleanup(terminal.Cancel)
	waitForPromptVisible(t, terminal, 8*time.Second)
	terminal.Send("\x0cc")
	var child string
	terminal.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		records, err := headless.NewStore(cfg).Reconcile()
		if err != nil {
			return err
		}
		if len(records) != 2 {
			return fmt.Errorf("have %d terminals, want parent and child", len(records))
		}
		for _, record := range records {
			if !record.Offline {
				return fmt.Errorf("offline startup default lost for %s", record.SessionID)
			}
			if h.HasHost(record.SessionID) {
				return fmt.Errorf("offline terminal %s unexpectedly published", record.SessionID)
			}
			if record.SessionID != source {
				child = record.SessionID
			}
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("child prompt missing")
		}
		return nil
	})
	terminal.Send("\x0co")
	terminal.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !h.HasHost(child) {
			return fmt.Errorf("child did not publish after explicit online toggle")
		}
		state, err := headless.NewStore(cfg).Load()
		if err != nil {
			return err
		}
		if state.Sessions[child].Offline || !state.Sessions[source].Offline {
			return fmt.Errorf("online toggle did not affect only child")
		}
		return nil
	})
}

func startManagementHostCLI(t *testing.T, h *ptytest.Harness, home string) *ptytest.PTYSession {
	return startManagementHostCLIWithAuthFile(t, h, home, "")
}

func startManagementHostCLIWithAuthFile(t *testing.T, h *ptytest.Harness, home, authFile string) *ptytest.PTYSession {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, 100, 24)
	resizeFD, err := syscall.Dup(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	resize := os.NewFile(uintptr(resizeFD), "management-host-resize")
	terminal := ptytest.NewPTYSession(t, master, resize, 100, 24)
	t.Cleanup(terminal.Close)
	args := []string{"--session", "management-host", "--endpoint", h.Endpoint(), "--insecure", "--shell", fixedPromptEmitRowsBash(t)}
	if authFile != "" {
		args = append(args, "--auth-file", authFile)
	} else {
		args = append(args, "--token", h.AccessToken())
	}
	cmd := exec.Command(buildLingonAttachBinary(t), args...)
	cmd.Env = testAttachCLIEnv(home)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	go func() { <-terminal.Context().Done(); _ = cmd.Process.Kill() }()
	go func() {
		err := cmd.Wait()
		if terminal.Context().Err() != nil {
			err = nil
		}
		terminal.SetRunErr(err)
	}()
	return terminal
}

func TestSessionManagementRelayAndHeadfulCLI(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	host := startManagementHostCLI(t, h, home)
	waitForPromptVisible(t, host, 8*time.Second)
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"management-host"}, 8*time.Second)
	host.Send("\x0ceHeadful renamed\r")
	if id, err := waitForSessionName(t, h.Clock(), h.Endpoint(), h.AccessToken(), "Headful renamed", 8*time.Second); err != nil || id != "management-host" {
		t.Fatalf("headful rename identity: %s %v", id, err)
	}
	client := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: "management-host", Insecure: true}
	// This API is available to owners even when another client holds terminal control.
	req := sessionmanage.Request{RequestID: "stable-create-request", SessionID: "management-host", Kind: "create"}
	started := time.Now()
	result, err := client.Manage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("relay creation ready in %s", time.Since(started))
	retry, err := client.Manage(context.Background(), req)
	if err != nil || retry.SessionID != result.SessionID {
		t.Fatalf("duplicate launched another terminal: %+v %v", retry, err)
	}
	records, err := headless.NewStore(cfg).Reconcile()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected one child: %+v %v", records, err)
	}
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"management-host", result.SessionID}, 8*time.Second)
	remote := startLingonAttachCLI(t, h, result.SessionID, 100, 24)
	t.Cleanup(remote.Cancel)
	waitForPromptVisible(t, remote, 8*time.Second)
	remote.Send("\x0ceRemote build\r")
	remote.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		state, err := headless.NewStore(cfg).Load()
		if err != nil {
			return err
		}
		if state.Sessions[result.SessionID].Name != "Remote build" {
			return fmt.Errorf("remote rename not persisted")
		}
		return nil
	})
	// Reconnect the actual child publisher through its test-owned local attach PTY.
	local := startLingonAttachHeadlessCLI(t, home, result.SessionID, 100, 24)
	t.Cleanup(local.Cancel)
	waitForPromptVisible(t, local, 8*time.Second)
	local.Send("\x0co")
	local.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if h.HasHost(result.SessionID) {
			return fmt.Errorf("child publisher has not disconnected")
		}
		return nil
	})
	local.Send("\x0co")
	local.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !h.HasHost(result.SessionID) {
			return fmt.Errorf("child publisher has not reconnected")
		}
		rows, err := fetchSessions(h.Endpoint(), h.AccessToken())
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.ID == result.SessionID && row.Name == "Remote build" {
				return nil
			}
		}
		return fmt.Errorf("rename was lost on publisher reconnect")
	})
	// Local attach took the terminal lease; use a fresh relay client for the survival assertion.
	local.Cancel()
	remote.Cancel()
	remote = startLingonAttachCLI(t, h, result.SessionID, 100, 24)
	t.Cleanup(remote.Cancel)
	waitForPromptVisible(t, remote, 8*time.Second)
	host.Cancel()
	remote.Send("echo REMOTE_CHILD_SURVIVED\r")
	remote.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("REMOTE_CHILD_SURVIVED") {
			return fmt.Errorf("detached child lost with headful host")
		}
		return nil
	})
}

func TestSessionManagementStartupPerformance(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.New()))
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	host := startManagementHostCLI(t, h, home)
	waitForPromptVisible(t, host, 8*time.Second)
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"management-host"}, 8*time.Second)
	client := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: "management-host", Insecure: true}
	var direct, remote []time.Duration
	// Alternate runs to keep executable/cache/host conditions comparable.
	for i := range 5 {
		id := fmt.Sprintf("direct-%d", i)
		cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--config-dir", cfg, "--session", id, "--shell", fixedPromptEmitRowsBash(t), "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure", "--cols", "100", "--rows", "24")
		cmd.Env = testAttachCLIEnv(home)
		before := time.Now()
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("direct launch: %v %s", err, output)
		}
		direct = append(direct, time.Since(before))
		before = time.Now()
		if _, err := client.Manage(context.Background(), sessionmanage.Request{RequestID: fmt.Sprintf("measure-%d", i), SessionID: "management-host", Kind: "create"}); err != nil {
			t.Fatal(err)
		}
		remote = append(remote, time.Since(before))
	}
	slices.Sort(direct)
	slices.Sort(remote)
	t.Logf("five alternating starts: direct -x median=%s routed creation median=%s added latency=%s", direct[2], remote[2], remote[2]-direct[2])
}

// This optional companion to the startup gate compares direct startup under one
// harness, alternating executable order and detaching each child before the next.
// Build the baseline with the same toolchain/flags as buildLingonAttachBinary.
func TestSessionManagementStartupBaselineComparison(t *testing.T) {
	baseline := os.Getenv("LINGON_TEST_STARTUP_BASELINE_BINARY")
	if baseline == "" {
		t.Skip("requires an explicitly built baseline executable")
	}
	h := newHarness(t, ptytest.WithClock(clock.New()))
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	fixed := buildLingonAttachBinary(t)
	// Match filesystem, directory, path length and permissions: executing one
	// binary from the checkout and another from the harness temp directory can
	// measure executable-location effects instead of the source change.
	copyExecutable := func(source, name string) string {
		t.Helper()
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(filepath.Dir(fixed), name)
		if err := os.WriteFile(target, data, 0o755); err != nil {
			t.Fatal(err)
		}
		return target
	}
	baseline = copyExecutable(baseline, "baseline-lingon-repro")
	fixed = copyExecutable(fixed, "modified-lingon-repro")
	var before, after []time.Duration
	for pair := 0; pair < 20; pair++ {
		executables := []string{baseline, fixed}
		if pair%2 != 0 {
			executables = []string{fixed, baseline}
		}
		for order, executable := range executables {
			id := fmt.Sprintf("paired-startup-%d-%d", pair, order)
			cmd := exec.Command(executable, "-x", "--config-dir", cfg, "--session", id, "--shell", fixedPromptEmitRowsBash(t), "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure", "--cols", "100", "--rows", "24")
			cmd.Env = testAttachCLIEnv(home)
			started := time.Now()
			output, err := cmd.CombinedOutput()
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("paired startup: %v %s", err, output)
			}
			if executable == baseline {
				before = append(before, elapsed)
			} else {
				after = append(after, elapsed)
			}
			if err := headless.DetachSession(context.Background(), cfg, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	slices.Sort(before)
	slices.Sort(after)
	median := func(samples []time.Duration) time.Duration { return (samples[9] + samples[10]) / 2 }
	t.Logf("twenty alternating direct-startup pairs: baseline median=%s p95=%s fixed median=%s p95=%s ratio=%.4f", median(before), before[18], median(after), after[18], float64(median(after))/float64(median(before)))
}
