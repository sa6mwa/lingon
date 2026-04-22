//go:build integration
// +build integration

package integrationptyattach_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

const ctrlDFlagEnv = "LINGON_TEST_CTRL_D_FLAG"

func TestMultiAttachCtrlLDSendsEOFAndCtrlDDetachesRelay(t *testing.T) {
	flagPath := filepath.Join(testutil.TempDir(t), "relay-eof.flag")
	t.Setenv(ctrlDFlagEnv, flagPath)

	recorder := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithClock(clock.New()), ptytest.WithWSRecorder(recorder))
	shellPath := writeCtrlDProbeShell(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:  "ctrl-d-relay",
		Shell:      shellPath,
		Cols:       80,
		Rows:       24,
		DisableRaw: true,
	})
	t.Cleanup(host.Cancel)
	waitUntil(t, h.Clock(), 8*time.Second, func() bool {
		return h.HasHost("ctrl-d-relay")
	})

	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "ctrl-d-relay",
		Cols:      80,
		Rows:      24,
	})
	waitUntil(t, h.Clock(), 8*time.Second, func() bool {
		return h.ClientCount("ctrl-d-relay") >= 1
	})

	attachA.SendBytes([]byte{0x04})
	if ok, err := attachA.WaitErr(5 * time.Second); !ok {
		t.Fatalf("attach did not exit after bare ctrl+d")
	} else if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("attach exit after bare ctrl+d: %v", err)
	}

	host.Wait(250 * time.Millisecond)
	if fileExists(flagPath) {
		t.Fatalf("bare ctrl+d unexpectedly delivered EOF to host session")
	}

	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "ctrl-d-relay",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attachB.Cancel)
	waitUntil(t, h.Clock(), 8*time.Second, func() bool {
		return h.ClientCount("ctrl-d-relay") >= 1
	})
	if fileExists(flagPath) {
		t.Fatalf("unexpected EOF marker before ctrl+l d")
	}

	attachB.SendCtrlL()
	attachB.Send("d")
	waitForFramePayload(t, h.Clock(), recorder, "client", "", ptytest.DirClientToServer, "command", 1, 3*time.Second)
	waitForFramePayload(t, h.Clock(), recorder, "host", "ctrl-d-relay", ptytest.DirServerToClient, "command", 1, 3*time.Second)
	if !waitForFileExists(h.Clock(), flagPath, 1500*time.Millisecond) {
		// Host-side shells may not yet be blocked in read(0) when the first
		// command lands; retry until EOF is observed to avoid timing flakes.
		delivered := false
		for i := 0; i < 6; i++ {
			attachB.SendCtrlL()
			attachB.Send("d")
			if waitForFileExists(h.Clock(), flagPath, 1500*time.Millisecond) {
				delivered = true
				break
			}
		}
		if !delivered {
			t.Fatalf("timed out waiting for EOF marker after ctrl+l d retries")
		}
	}
}

func TestMultiAttachCtrlLDSendsEOFAndCtrlDDetachesHeadlessLocal(t *testing.T) {
	flagPath := filepath.Join(testutil.TempDir(t), "headless-eof.flag")
	t.Setenv(ctrlDFlagEnv, flagPath)

	cfgDir := testutil.TempDir(t)
	shellPath := writeCtrlDProbeShell(t)

	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "ctrl-d-local",
		Shell:     shellPath,
		Respawn:   true,
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	h := newHarness(t, ptytest.WithClock(clock.New()))
	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "ctrl-d-local",
		Cols:               100,
		Rows:               28,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	attachA.Wait(250 * time.Millisecond)

	attachA.SendBytes([]byte{0x04})
	if ok, err := attachA.WaitErr(5 * time.Second); !ok {
		t.Fatalf("local attach did not exit after bare ctrl+d")
	} else if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("local attach exit after bare ctrl+d: %v", err)
	}
	if fileExists(flagPath) {
		t.Fatalf("bare ctrl+d unexpectedly delivered EOF to local headless session")
	}

	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          "ctrl-d-local",
		Cols:               100,
		Rows:               28,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attachB.Cancel)
	attachB.Wait(250 * time.Millisecond)
	if fileExists(flagPath) {
		t.Fatalf("unexpected EOF marker before local ctrl+l d")
	}

	attachB.SendCtrlL()
	attachB.Send("d")
	waitUntil(t, h.Clock(), 8*time.Second, func() bool {
		return fileExists(flagPath)
	})
}

func writeCtrlDProbeShell(t *testing.T) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"flag=\"$" + ctrlDFlagEnv + "\"\n" +
		"old=$(stty -g 2>/dev/null || true)\n" +
		"stty -echo -icanon min 1 time 0 2>/dev/null || true\n" +
		"byte=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d ' \\n')\n" +
		"if [ -n \"$old\" ]; then stty \"$old\" 2>/dev/null || true; fi\n" +
		"if [ \"$byte\" = \"4\" ] && [ -n \"$flag\" ]; then\n" +
		"  printf '1\\n' >\"$flag\"\n" +
		"fi\n" +
		"while :; do sleep 1; done\n"
	path := filepath.Join(testutil.TempDir(t), "ctrl-d-probe.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write ctrl-d probe shell: %v", err)
	}
	return path
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func waitForFileExists(clk clock.Clock, path string, timeout time.Duration) bool {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		if fileExists(path) {
			return true
		}
		ptytest.Advance(clk, 25*time.Millisecond)
	}
	return fileExists(path)
}
