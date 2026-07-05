//go:build integration
// +build integration

package integrationptyattach_test

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

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
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

func TestRealCLIRelayHeadlessInitialConnectAndWinchResizePTY(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionID = "relay-headless-realcli-connect-size"
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
	attach.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.Row(0), "wall inactivity off") {
			return fmt.Errorf("unexpected wall inactivity banner on initial relay headless connect: %q", screen.Row(0))
		}
		return nil
	})
	assertHeadlessPTYSizeViaShell(t, attach, 40, 10, "connect-size")

	attach.Resize(52, 14)
	ptytest.Advance(attach.Clock(), 250*time.Millisecond)
	assertHeadlessPTYSizeViaShell(t, attach, 52, 14, "post-winch-size")
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
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
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

func TestRealCLILingonXAttachLocalHeadlessRendersPrompt(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	const sessionID = "local-headless-lingonx-attach"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonXAttachHeadlessCLI(t, homeDir, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
}

func TestRealCLILingonXAttachWithoutSessionIDRendersSingleLocalHeadlessPrompt(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	const sessionID = "local-headless-lingonx-default"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonXAttachHeadlessCLI(t, homeDir, "", 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
}

func TestRealCLILingonXStartedHeadlessThenLingonXAttachRendersPrompt(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	const sessionID = "local-headless-real-lingonx-start"
	startLingonXHeadlessSessionCLI(t, homeDir, sessionID)
	t.Cleanup(func() {
		_ = headless.DetachSession(context.Background(), cfgDir, sessionID)
	})

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonXAttachHeadlessCLI(t, homeDir, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
}

func TestRealCLILingonXOfflineStartsLocalHeadlessWithConfiguredEndpointLoggedOut(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(cfgDir, lingon.DefaultConfigFileName)
	if err := os.WriteFile(configPath, []byte("client:\n  endpoint: https://configured.example/v1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	const sessionID = "local-headless-lingonx-offline-configured"
	aliasPath := lingonXAliasPath(t)
	cmd := exec.Command(
		aliasPath,
		"-o",
		"--session", sessionID,
		"--shell", fixedPromptEmitRowsBash(t),
		"--geometry", "40x10",
		"--disable-desktop-notifications",
	)
	cmd.Env = testAttachCLIEnv(homeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start lingonx -o headless session: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "headless session starting in background") {
		t.Fatalf("unexpected lingonx -o start output: %s", string(output))
	}
	t.Cleanup(func() {
		_ = headless.DetachSession(context.Background(), cfgDir, sessionID)
	})

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonXAttachHeadlessCLI(t, homeDir, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
}

func TestRealCLILingonXReportsMissingRelayAuthBeforeDetachedExit(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	aliasPath := lingonXAliasPath(t)
	missingAuth := filepath.Join(homeDir, "missing-auth.json")
	cmd := exec.Command(
		aliasPath,
		"--session", "headless-missing-relay-auth",
		"--shell", fixedPromptEmitRowsBash(t),
		"--endpoint", "https://relay.example/v1",
		"--auth-file", missingAuth,
		"--disable-desktop-notifications",
	)
	cmd.Env = testAttachCLIEnv(homeDir)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected lingonx startup to fail without relay auth, output:\n%s", string(output))
	}
	text := string(output)
	if !strings.Contains(text, "headless startup failed") {
		t.Fatalf("startup failure did not name headless startup:\n%s", text)
	}
	if !strings.Contains(text, "auth file not found") {
		t.Fatalf("startup failure did not report missing auth:\n%s", text)
	}
	if !strings.Contains(text, "--offline") {
		t.Fatalf("startup failure did not suggest local-only offline startup:\n%s", text)
	}
	if strings.Contains(text, "headless session starting in background") {
		t.Fatalf("startup reported background success despite missing auth:\n%s", text)
	}
}

func TestRealCLIRootHeadlessAttachLocalHeadlessRendersPrompt(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	const sessionID = "local-headless-root-x-attach"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stop)

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	attach := startLingonRootHeadlessAttachCLI(t, homeDir, sessionID, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
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
	if !screenContainsWithinRealTime(attach, "Not connected", 2*time.Second) {
		t.Fatalf("expected dead relay headless session to remain visible during normal grace:\n%s", attach.Screen().String())
	}
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return fmt.Errorf("stale disconnect overlay remained after dead tab removal:\n%s", screen.String())
		}
		return nil
	})

	const token = "RELAY_HEADLESS_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	if !screenContainsWithinRealTime(attach, token, 2*time.Second) {
		t.Fatalf("surviving relay headless tab did not remain interactive:\n%s", attach.Screen().String())
	}
}

func TestRealCLIRelayHeadlessExitRemovesTerminatedSessionWithoutReconnectOverlay(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionA = "relay-headless-exit-survivor"
	const sessionB = "relay-headless-exit-active"
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

	waitForSessionIDsExact(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA, sessionB}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionA: true, sessionB: true}, 8*time.Second)

	attach := startLingonAttachCLI(t, h, sessionB, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	attach.Send("exit\r")

	waitForSessionIDsExact(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA}, 8*time.Second)
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if strings.Contains(row0, "wall inactivity off") {
			return fmt.Errorf("unexpected stale wall inactivity banner after explicit headless exit: %q", row0)
		}
		if strings.Contains(screen.String(), "Not connected") || strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
			return fmt.Errorf("unexpected reconnect overlay after explicit headless exit:\n%s", screen.String())
		}
		if strings.Contains(row0, sessionB) {
			return fmt.Errorf("terminated relay headless tab %q still present: %q", sessionB, row0)
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("surviving relay headless prompt missing after explicit exit:\n%s", screen.String())
		}
		return nil
	})

	const token = "RELAY_HEADLESS_EXIT_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	attach.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(token) {
			return fmt.Errorf("surviving relay headless session was not interactive after explicit exit:\n%s", screen.String())
		}
		return nil
	})
}

func TestRealCLIRelayHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t, ptytest.WithClock(clock.New()))
	const sessionA = "relay-headless-detach-survivor"
	const sessionB = "relay-headless-detach-active"
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: shortConfigDir(t),
		SessionID: sessionA,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopA)
	cfgB := shortConfigDir(t)
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgB,
		SessionID: sessionB,
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopB)

	waitForSessionIDsExact(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA, sessionB}, 8*time.Second)
	waitForHeadlessFlags(t, h.Clock(), h.Endpoint(), h.AccessToken(), map[string]bool{sessionA: true, sessionB: true}, 8*time.Second)

	attach := startLingonAttachCLI(t, h, sessionB, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	if err := headless.DetachSession(context.Background(), cfgB, sessionB); err != nil {
		t.Fatalf("DetachSession(%q): %v", sessionB, err)
	}

	waitForSessionIDsExact(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionA}, 8*time.Second)
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if strings.Contains(row0, "wall inactivity off") {
			return fmt.Errorf("unexpected stale wall inactivity banner after detach: %q", row0)
		}
		if strings.Contains(screen.String(), "Not connected") || strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
			return fmt.Errorf("unexpected reconnect overlay after explicit headless detach:\n%s", screen.String())
		}
		if strings.Contains(row0, sessionB) {
			return fmt.Errorf("detached relay headless tab %q still present: %q", sessionB, row0)
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("surviving relay headless prompt missing after detach:\n%s", screen.String())
		}
		return nil
	})

	const token = "RELAY_HEADLESS_DETACH_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	attach.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(token) {
			return fmt.Errorf("surviving relay headless session was not interactive after detach:\n%s", screen.String())
		}
		return nil
	})
}

func TestRealCLILocalHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
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
		if strings.Contains(screen.Row(0), sessionB) {
			return fmt.Errorf("dead local headless tab %q still present after grace: %q", sessionB, screen.Row(0))
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

func TestRealCLILocalHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := shortHomeDir(t)
	cfgDir := filepath.Join(homeDir, lingon.DefaultConfigDirName)
	const sessionA = "local-headless-detach-survivor"
	const sessionB = "local-headless-detach-active"
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionA,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopA)
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionB,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	t.Cleanup(stopB)

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionIDsExact(cfgDir, []string{sessionA, sessionB})
	})

	attach := startLingonAttachHeadlessCLI(t, homeDir, sessionB, 40, 10)
	defer attach.Cancel()
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	if err := headless.DetachSession(context.Background(), cfgDir, sessionB); err != nil {
		t.Fatalf("DetachSession(%q): %v", sessionB, err)
	}

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionIDsExact(cfgDir, []string{sessionA})
	})
	attach.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if strings.Contains(row0, "wall inactivity off") {
			return fmt.Errorf("unexpected stale wall inactivity banner after local detach: %q", row0)
		}
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return fmt.Errorf("unexpected reconnect overlay after local detach:\n%s", screen.String())
		}
		if strings.Contains(row0, sessionB) {
			return fmt.Errorf("detached local headless tab %q still present: %q", sessionB, row0)
		}
		if !screen.Contains("PROMPT>") {
			return fmt.Errorf("surviving local headless prompt missing after detach:\n%s", screen.String())
		}
		return nil
	})

	const token = "LOCAL_HEADLESS_DETACH_SURVIVOR_OK"
	attach.Send("echo " + token + "\r")
	if !screenContainsWithinRealTime(attach, token, 2*time.Second) {
		t.Fatalf("surviving local headless session was not interactive after detach:\n%s", attach.Screen().String())
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

func startLingonRootHeadlessAttachCLI(t *testing.T, homeDir, sessionID string, cols, rows int) *ptytest.PTYSession {
	t.Helper()
	return startLingonLocalHeadlessAttachCLI(t, homeDir, sessionID, cols, rows, buildLingonAttachBinary(t), "-x", "attach")
}

func startLingonXHeadlessSessionCLI(t *testing.T, homeDir, sessionID string) {
	t.Helper()
	aliasPath := lingonXAliasPath(t)
	cmd := exec.Command(
		aliasPath,
		"--session", sessionID,
		"--shell", fixedPromptEmitRowsBash(t),
		"--geometry", "40x10",
		"--disable-desktop-notifications",
	)
	cmd.Env = testAttachCLIEnv(homeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("start lingonx headless session: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "headless session starting in background") {
		t.Fatalf("unexpected lingonx start output: %s", string(output))
	}
}

func startLingonXAttachHeadlessCLI(t *testing.T, homeDir, sessionID string, cols, rows int) *ptytest.PTYSession {
	t.Helper()
	aliasPath := lingonXAliasPath(t)
	return startLingonLocalHeadlessAttachCLI(t, homeDir, sessionID, cols, rows, aliasPath, "attach")
}

func lingonXAliasPath(t *testing.T) string {
	t.Helper()
	aliasDir := t.TempDir()
	aliasPath := filepath.Join(aliasDir, "lingonx")
	if err := os.Symlink(buildLingonAttachBinary(t), aliasPath); err != nil {
		t.Fatalf("symlink lingonx alias: %v", err)
	}
	return aliasPath
}

func startLingonLocalHeadlessAttachCLI(t *testing.T, homeDir, sessionID string, cols, rows int, bin string, prefixArgs ...string) *ptytest.PTYSession {
	t.Helper()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	master, slave := ptytest.OpenPTY(t, cols, rows)
	resizeFD, err := syscall.Dup(int(slave.Fd()))
	if err != nil {
		t.Fatalf("dup lingonx attach slave pty: %v", err)
	}
	resizeSlave := os.NewFile(uintptr(resizeFD), slave.Name()+"-resize")
	sess := ptytest.NewPTYSession(t, master, resizeSlave, cols, rows)
	t.Cleanup(func() {
		_ = master.Close()
		_ = resizeSlave.Close()
	})

	args := append([]string(nil), prefixArgs...)
	if sessionID != "" {
		args = append(args, sessionID)
	}
	args = append(args, "--request-control", "--disable-desktop-notifications")
	cmd := exec.Command(bin, args...)
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
		t.Fatalf("start local headless attach cli: %v", err)
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
