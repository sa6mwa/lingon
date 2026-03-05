package session_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

const sessionCtrlDFlagEnv = "LINGON_TEST_SESSION_CTRL_D_FLAG"

func TestCtrlLDSendsEOFOnRemoteTab(t *testing.T) {
	flagPath := filepath.Join(t.TempDir(), "session-remote-eof.flag")
	t.Setenv(sessionCtrlDFlagEnv, flagPath)

	h := newHarness(t, ptytest.WithClock(clock.New()))

	targetShell := writeSessionCtrlDProbeShell(t)
	controllerShell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		controllerShell = "/bin/bash"
	}

	target := h.StartHost(ptytest.HostOptions{
		SessionID:   "ctrl-d-remote-target",
		SessionName: "ctrl-d-remote-target",
		Shell:       targetShell,
		Cols:        100,
		Rows:        30,
		DisableRaw:  true,
	})
	t.Cleanup(target.Cancel)

	controller := h.StartHost(ptytest.HostOptions{
		SessionID:   "ctrl-d-remote-controller",
		SessionName: "ctrl-d-remote-controller",
		Shell:       controllerShell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(controller.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 8*time.Second)

	controller.SendCtrlL()
	controller.Send("n")
	advanceTestClock(controller.Clock(), 200*time.Millisecond)

	deadline := controller.Clock().Now().Add(4 * time.Second)
	for controller.Clock().Now().Before(deadline) {
		if h.ClientCount("ctrl-d-remote-target") >= 1 {
			break
		}
		advanceTestClock(controller.Clock(), 50*time.Millisecond)
	}
	if h.ClientCount("ctrl-d-remote-target") < 1 {
		t.Fatalf("timed out waiting for remote tab attach client")
	}

	if sessionFileExists(flagPath) {
		t.Fatalf("unexpected EOF marker before ctrl+l d")
	}

	controller.SendCtrlL()
	controller.Send("d")
	if !waitForSessionFileExists(controller.Clock(), flagPath, 2*time.Second) {
		t.Fatalf("timed out waiting for EOF marker after ctrl+l d on remote tab")
	}
}

func writeSessionCtrlDProbeShell(t *testing.T) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"flag=\"$" + sessionCtrlDFlagEnv + "\"\n" +
		"old=$(stty -g 2>/dev/null || true)\n" +
		"stty -echo -icanon min 1 time 0 2>/dev/null || true\n" +
		"byte=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d ' \\n')\n" +
		"if [ -n \"$old\" ]; then stty \"$old\" 2>/dev/null || true; fi\n" +
		"if [ \"$byte\" = \"4\" ] && [ -n \"$flag\" ]; then\n" +
		"  printf '1\\n' >\"$flag\"\n" +
		"fi\n" +
		"printf 'received=%s\\n' \"$byte\"\n" +
		"while :; do sleep 1; done\n"
	path := filepath.Join(t.TempDir(), "session-ctrl-d-probe.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write ctrl-d probe shell: %v", err)
	}
	return path
}

func sessionFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func waitForSessionFileExists(clk clock.Clock, path string, timeout time.Duration) bool {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if sessionFileExists(path) {
			return true
		}
		ptytest.Advance(clk, 25*time.Millisecond)
	}
	return sessionFileExists(path)
}
