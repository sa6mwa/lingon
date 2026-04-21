package attach_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRealCLIRelayHeadlessResizeMatrixRendersExpectedViewport(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionID = "relay-headless-realcli-matrix"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionID: true}, 8*time.Second)

	attach := startLingonAttachCLI(t, h, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	runMultiHeadlessResizeMatrix(t, attach, func(cols, rows int) { attach.Resize(cols, rows) })
}

func TestRelayHeadlessMultiAttachReceivesInitialSnapshot(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionID = "relay-headless-initial-snapshot"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionID,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionID: true}, 8*time.Second)

	var captured *attach.Client
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      40,
		Rows:      10,
		OnView: func(id string, client *attach.Client) {
			if id == sessionID {
				captured = client
			}
		},
	})
	t.Cleanup(attach.Cancel)

	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if captured == nil {
			return fmt.Errorf("attach view not created yet")
		}
		if !captured.HasSnapshot() {
			return fmt.Errorf("attach client still has no snapshot:\n%s", screen.String())
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("initial relay headless prompt not rendered:\n%s", screen.String())
		}
		return nil
	})
}

func TestRelayHeadlessMultiAttachExplicitSessionReceivesInitialSnapshotWithPeers(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	const sessionA = "relay-headless-peer-a"
	const sessionB = "relay-headless-peer-b"
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionA,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopA)
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionB,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopB)

	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA, sessionB}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionA: true, sessionB: true}, 8*time.Second)

	var captured *attach.Client
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionB,
		Cols:      40,
		Rows:      10,
		OnView: func(id string, client *attach.Client) {
			if id == sessionB {
				captured = client
			}
		},
	})
	t.Cleanup(attach.Cancel)

	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if captured == nil {
			return fmt.Errorf("attach view for %s not created yet", sessionB)
		}
		if !captured.HasSnapshot() {
			return fmt.Errorf("attach client for %s still has no snapshot:\n%s", sessionB, screen.String())
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("explicit relay headless session %s prompt not rendered with peers:\n%s", sessionB, screen.String())
		}
		return nil
	})
}

func TestRealCLILocalHeadlessResizeMatrixRendersExpectedViewport(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, ".lingon")
	const sessionID = "local-headless-realcli-matrix"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonAttachHeadlessCLI(t, homeDir, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	runMultiHeadlessResizeMatrix(t, attach, func(cols, rows int) { attach.Resize(cols, rows) })
}

func TestRealCLIRelayHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionA = "relay-headless-survivor"
	const sessionB = "relay-headless-dead"
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionB,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	stoppedB := false
	t.Cleanup(func() {
		if !stoppedB {
			stopB()
		}
	})

	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionB}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionB: true}, 8*time.Second)

	attach := startLingonAttachCLI(t, h, sessionB, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionA,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopA)
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA, sessionB}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionA: true, sessionB: true}, 8*time.Second)

	stopB()
	stoppedB = true

	waitForSessionIDsExact(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA}, 8*time.Second)
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return fmt.Errorf("stale disconnect overlay remained after dead tab removal:\n%s", screen.String())
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("surviving headless session prompt missing after active session death:\n%s", screen.String())
		}
		return nil
	})

	const token = "RELAY_HEADLESS_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	if !screenContainsWithinRealTime(attach, token, 2*time.Second) {
		t.Fatalf("surviving relay headless tab did not remain interactive:\n%s", attach.Screen().String())
	}
}

func TestRealCLILocalHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, ".lingon")
	const sessionA = "local-headless-survivor"
	const sessionB = "local-headless-dead"
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionB,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	stoppedB := false
	t.Cleanup(func() {
		if !stoppedB {
			stopB()
		}
	})

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonAttachHeadlessCLI(t, homeDir, sessionB, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionA,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopA)
	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})

	stopB()
	stoppedB = true

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionIDsExact(cfgDir, []string{sessionA})
	})
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return fmt.Errorf("stale disconnect overlay remained after local dead tab removal:\n%s", screen.String())
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("surviving local headless session prompt missing after active session death:\n%s", screen.String())
		}
		return nil
	})

	const token = "LOCAL_HEADLESS_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	if !screenContainsWithinRealTime(attach, token, 2*time.Second) {
		t.Fatalf("surviving local headless tab did not remain interactive:\n%s", attach.Screen().String())
	}
}

func startLingonAttachHeadlessCLI(t *testing.T, homeDir, sessionID string, cols, rows int) *ptytest.PTYSession {
	t.Helper()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	bin := buildLingonAttachBinary(t)
	master, slave := ptytest.OpenPTY(t, cols, rows)
	resizeFD, err := syscall.Dup(int(slave.Fd()))
	if err != nil {
		t.Fatalf("dup local headless attach slave pty: %v", err)
	}
	resizeSlave := os.NewFile(uintptr(resizeFD), slave.Name()+"-resize")
	sess := ptytest.NewPTYSession(t, master, resizeSlave, cols, rows)
	t.Cleanup(func() {
		_ = master.Close()
		_ = resizeSlave.Close()
	})

	cmd := exec.Command(
		bin,
		"attach",
		"--headless",
		sessionID,
		"--request-control",
		"--disable-desktop-notifications",
	)
	cmd.Env = testAttachCLIEnv(homeDir)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lingon attach --headless cli: %v", err)
	}
	_ = slave.Close()
	go func() {
		<-sess.Context().Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		err := cmd.Wait()
		if sess.Context().Err() != nil && (err == nil || errors.Is(err, os.ErrProcessDone) || strings.Contains(err.Error(), "signal: killed")) {
			err = nil
		}
		sess.SetRunErr(err)
	}()
	return sess
}

func waitForSessionIDsExact(t *testing.T, clk clock.Clock, endpoint, token string, ids []string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			found, err := fetchSessionIDs(endpoint, token)
			t.Fatalf("timed out waiting for exact sessions %v (have %v, err=%v)", ids, found, err)
		}
		found, err := fetchSessionIDs(endpoint, token)
		if err == nil && foundMatchesExactly(found, ids) {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func localSessionIDsExact(cfgDir string, ids []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	source := localHeadlessSessionSource(cfgDir)
	sessions, err := source(ctx)
	if err != nil {
		return false
	}
	found := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		found[session.ID] = true
	}
	return foundMatchesExactly(found, ids)
}

func waitForHeadlessFlags(t *testing.T, clk clock.Clock, endpoint, token string, want map[string]bool, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for {
		if ptytest.Now(clk).After(deadline) {
			rows, err := fetchSessions(endpoint, token)
			t.Fatalf("timed out waiting for headless flags %v (have %#v, err=%v)", want, rows, err)
		}
		rows, err := fetchSessions(endpoint, token)
		if err == nil && headlessFlagsMatch(rows, want) {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
}

func headlessFlagsMatch(rows []sessionRow, want map[string]bool) bool {
	if len(rows) != len(want) {
		return false
	}
	for _, row := range rows {
		headless, ok := want[row.ID]
		if !ok || row.Headless != headless {
			return false
		}
	}
	return true
}

func foundMatchesExactly(found map[string]bool, ids []string) bool {
	if len(found) != len(ids) {
		return false
	}
	for _, id := range ids {
		if !found[id] {
			return false
		}
	}
	return true
}

func shortHomeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lingon-home-")
	if err != nil {
		t.Fatalf("MkdirTemp home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
