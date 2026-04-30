//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/pslog"
)

func TestHostRemoteHeadlessInitialConnectAndWinchResizePTY(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	viewer := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-remote-headless-viewer",
		SessionName: "host-remote-headless-viewer",
		Shell:       "/bin/bash",
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(viewer.Cancel)
	viewer.Send("echo LOCAL_VIEWER_READY\n")
	if !screenContainsWithin(viewer, "LOCAL_VIEWER_READY", 2*time.Second) {
		t.Fatalf("local host viewer not interactive before remote switch:\n%s", viewer.Screen().String())
	}

	stop := startRelayHeadlessDaemon(t, relayHeadlessDaemonSpec{
		ConfigDir: shortHeadlessConfigDir(t),
		SessionID: "host-remote-headless-target",
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		TLSDir:    h.TLSDir(),
		Shell:     fixedPromptEmitRowsBashWithPromptSession(t, "HREMOTE>"),
		Clock:     h.Clock(),
	})
	t.Cleanup(stop)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 8*time.Second)
	switchHostToHeadlessRemote(t, h, viewer, "HREMOTE>")
	viewer.Eventually(2*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if strings.Contains(screen.Row(0), "wall inactivity off") {
			return fmt.Errorf("unexpected wall inactivity banner on initial remote headless connect: %q", screen.Row(0))
		}
		return nil
	})

	assertRemoteHeadlessPTYSize(t, viewer, "10 40", "connect-size")

	viewer.Resize(52, 14)
	advanceTestClock(h.Clock(), 250*time.Millisecond)
	assertRemoteHeadlessPTYSize(t, viewer, "14 52", "post-winch-size")
}

func TestHostRemoteHeadlessExitRemovesSessionWithoutReconnectOverlay(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	viewer := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-remote-headless-exit-viewer",
		SessionName: "host-remote-headless-exit-viewer",
		Shell:       "/bin/bash",
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(viewer.Cancel)
	viewer.Send("echo LOCAL_VIEWER_READY\n")
	if !screenContainsWithin(viewer, "LOCAL_VIEWER_READY", 2*time.Second) {
		t.Fatalf("local host viewer not interactive before remote switch:\n%s", viewer.Screen().String())
	}

	stop := startRelayHeadlessDaemon(t, relayHeadlessDaemonSpec{
		ConfigDir: shortHeadlessConfigDir(t),
		SessionID: "host-remote-headless-exit-target",
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		TLSDir:    h.TLSDir(),
		Shell:     fixedPromptEmitRowsBashWithPromptSession(t, "HREMOTE>"),
		Clock:     h.Clock(),
	})
	t.Cleanup(stop)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 8*time.Second)
	switchHostToHeadlessRemote(t, h, viewer, "HREMOTE>")

	viewer.Send("exit\n")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 8*time.Second)

	viewer.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if strings.Contains(screen.String(), "Not connected") || strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
			return fmt.Errorf("unexpected reconnect overlay after explicit remote headless exit:\n%s", screen.String())
		}
		if strings.Contains(row0, "host-remote-headless-exit-target") {
			return fmt.Errorf("terminated remote headless tab still present: %q", row0)
		}
		if strings.Contains(screen.String(), "HREMOTE>") {
			return fmt.Errorf("stale remote headless prompt remained after exit:\n%s", screen.String())
		}
		return nil
	})

	const token = "HOST_REMOTE_HEADLESS_EXIT_LOCAL_OK"
	viewer.Send("echo " + token + "\n")
	if !screenContainsWithin(viewer, token, 2*time.Second) {
		t.Fatalf("local host session was not interactive after remote headless exit:\n%s", viewer.Screen().String())
	}
}

func TestHostRemoteHeadlessDetachRemovesSessionWithoutReconnectOverlay(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	viewer := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-remote-headless-detach-viewer",
		SessionName: "host-remote-headless-detach-viewer",
		Shell:       "/bin/bash",
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(viewer.Cancel)
	viewer.Send("echo LOCAL_VIEWER_READY\n")
	if !screenContainsWithin(viewer, "LOCAL_VIEWER_READY", 2*time.Second) {
		t.Fatalf("local host viewer not interactive before remote switch:\n%s", viewer.Screen().String())
	}

	cfgDir := shortHeadlessConfigDir(t)
	stop := startRelayHeadlessDaemon(t, relayHeadlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "host-remote-headless-detach-target",
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		TLSDir:    h.TLSDir(),
		Shell:     fixedPromptEmitRowsBashWithPromptSession(t, "HREMOTE>"),
		Clock:     h.Clock(),
	})
	t.Cleanup(stop)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 8*time.Second)
	switchHostToHeadlessRemote(t, h, viewer, "HREMOTE>")

	if err := headless.DetachSession(context.Background(), cfgDir, "host-remote-headless-detach-target"); err != nil {
		t.Fatalf("DetachSession: %v", err)
	}
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 8*time.Second)

	viewer.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row0 := screen.Row(0)
		if strings.Contains(screen.String(), "Not connected") || strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
			return fmt.Errorf("unexpected reconnect overlay after remote headless detach:\n%s", screen.String())
		}
		if strings.Contains(row0, "host-remote-headless-detach-target") {
			return fmt.Errorf("detached remote headless tab still present: %q", row0)
		}
		if strings.Contains(screen.String(), "HREMOTE>") {
			return fmt.Errorf("stale remote headless prompt remained after detach:\n%s", screen.String())
		}
		return nil
	})

	const token = "HOST_REMOTE_HEADLESS_DETACH_LOCAL_OK"
	viewer.Send("echo " + token + "\n")
	if !screenContainsWithin(viewer, token, 2*time.Second) {
		t.Fatalf("local host session was not interactive after remote headless detach:\n%s", viewer.Screen().String())
	}
}

func TestHostRemoteHeadlessReacquiresControlAndResizesAfterAttachControllerDisconnects(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	viewer := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-remote-headless-reacquire-viewer",
		SessionName: "host-remote-headless-reacquire-viewer",
		Shell:       "/bin/bash",
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(viewer.Cancel)
	viewer.Send("echo LOCAL_VIEWER_READY\n")
	if !screenContainsWithin(viewer, "LOCAL_VIEWER_READY", 2*time.Second) {
		t.Fatalf("local host viewer not interactive before remote switch:\n%s", viewer.Screen().String())
	}

	stop := startRelayHeadlessDaemon(t, relayHeadlessDaemonSpec{
		ConfigDir: shortHeadlessConfigDir(t),
		SessionID: "host-remote-headless-reacquire-target",
		Publish:   true,
		Endpoint:  h.Endpoint(),
		Token:     h.AccessToken(),
		TLSDir:    h.TLSDir(),
		Shell:     fixedPromptEmitRowsBashWithPromptSession(t, "HREMOTE>"),
		Clock:     h.Clock(),
	})
	t.Cleanup(stop)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 8*time.Second)
	switchHostToHeadlessRemote(t, h, viewer, "HREMOTE>")
	assertRemoteHeadlessPTYSize(t, viewer, "10 40", "initial-host-remote-size")

	controller := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-remote-headless-reacquire-target",
		Cols:      52,
		Rows:      14,
	})
	t.Cleanup(controller.Cancel)
	controller.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("HREMOTE>") {
			return fmt.Errorf("waiting for attach controller prompt:\n%s", screen.String())
		}
		return nil
	})
	controller.Send("stty size; echo __ATTACH_REACQUIRE_SIZE__\n")
	controller.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("__ATTACH_REACQUIRE_SIZE__") {
			return fmt.Errorf("waiting for attach controller size marker:\n%s", screen.String())
		}
		if !screen.Contains("14 52") {
			return fmt.Errorf("expected attach controller to resize headless PTY to 14 52, got:\n%s", screen.String())
		}
		return nil
	})

	controller.Cancel()
	advanceTestClock(h.Clock(), 500*time.Millisecond)

	assertRemoteHeadlessPTYSize(t, viewer, "10 40", "post-attach-disconnect-reacquire")
}

type relayHeadlessDaemonSpec struct {
	ConfigDir string
	SessionID string
	Publish   bool
	Endpoint  string
	Token     string
	TLSDir    string
	Shell     string
	Clock     clock.Clock
}

func startRelayHeadlessDaemon(t *testing.T, spec relayHeadlessDaemonSpec) func() {
	t.Helper()
	runCtx, cancelRun := context.WithCancel(context.Background())
	d := headlessd.New(headlessd.Options{
		ConfigDir: spec.ConfigDir,
		SessionID: spec.SessionID,
		Publish:   spec.Publish,
		Endpoint:  spec.Endpoint,
		Token:     spec.Token,
		TLSDir:    spec.TLSDir,
		Shell:     spec.Shell,
		Clock:     spec.Clock,
		Logger:    pslog.NoopLogger(),
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run(runCtx)
	}()

	socketPath, err := headless.SocketPath(spec.ConfigDir, spec.SessionID)
	if err != nil {
		t.Fatalf("SocketPath(%q): %v", spec.SessionID, err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-runErr:
			if err != nil && err != context.Canceled {
				t.Fatalf("headless daemon %s failed before socket ready: %v", spec.SessionID, err)
			}
			t.Fatalf("headless daemon %s exited before socket ready", spec.SessionID)
		default:
		}
		if headless.SocketExists(socketPath) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !headless.SocketExists(socketPath) {
		t.Fatalf("headless daemon %s socket not ready: %s", spec.SessionID, socketPath)
	}

	return func() {
		cancelRun()
		select {
		case err := <-runErr:
			if err != nil && err != context.Canceled {
				t.Fatalf("headless daemon %s run: %v", spec.SessionID, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("headless daemon %s did not stop", spec.SessionID)
		}
	}
}

func fixedPromptEmitRowsBashWithPromptSession(t *testing.T, prompt string) string {
	t.Helper()
	rcPath := filepath.Join(t.TempDir(), "lingon-session-headless-rc")
	rc := strings.Join([]string{
		"export PS1=" + shellQuoteSession(prompt),
		"emitrows() {",
		"  local n=\"$1\"",
		"  local i=1",
		"  while [ \"$i\" -le \"$n\" ]; do",
		"    printf 'ROW-%02d\\n' \"$i\"",
		"    i=$((i+1))",
		"  done",
		"}",
	}, "\n") + "\n"
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatalf("write headless rc file: %v", err)
	}
	shellPath := filepath.Join(t.TempDir(), "lingon-session-headless-shell.sh")
	script := "#!/usr/bin/env bash\nexec /bin/bash --noprofile --rcfile " + shellQuoteSession(rcPath) + " -i\n"
	if err := os.WriteFile(shellPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write headless shell wrapper: %v", err)
	}
	return shellPath
}

func shellQuoteSession(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shortHeadlessConfigDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lingon-session-headless-")
	if err != nil {
		t.Fatalf("MkdirTemp headless config: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func switchHostToHeadlessRemote(t *testing.T, h *ptytest.Harness, viewer *ptytest.PTYSession, remotePrompt string) {
	t.Helper()
	switched := false
	for i := 0; i < 10; i++ {
		viewer.SendCtrlL()
		viewer.Send("n")
		advanceTestClock(h.Clock(), 350*time.Millisecond)
		if screenContainsWithin(viewer, remotePrompt, 2*time.Second) {
			switched = true
			break
		}
	}
	if !switched {
		t.Fatalf("timed out switching host to remote headless tab:\n%s", viewer.Screen().String())
	}
}

func assertRemoteHeadlessPTYSize(t *testing.T, viewer *ptytest.PTYSession, want, phase string) {
	t.Helper()
	token := "__REMOTE_HEADLESS_SIZE__"
	viewer.Send("stty size; echo " + token + "\n")
	viewer.Eventually(4*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.String(), token) {
			return fmt.Errorf("waiting for remote headless size token in %s:\n%s", phase, screen.String())
		}
		if !strings.Contains(screen.String(), want) {
			return fmt.Errorf("expected remote headless PTY size %s in %s, got:\n%s", want, phase, screen.String())
		}
		return nil
	})
}
