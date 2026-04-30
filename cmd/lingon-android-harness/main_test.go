package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestBuildHostSessionOptionsDisablesDesktopNotifications(t *testing.T) {
	t.Parallel()

	stdinFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open stdin: %v", err)
	}
	t.Cleanup(func() { _ = stdinFile.Close() })

	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open stdout: %v", err)
	}
	t.Cleanup(func() { _ = stdoutFile.Close() })

	var (
		ptyMu  sync.Mutex
		ptyBuf bytes.Buffer
	)
	opts := buildHostSessionOptions(
		"https://example.test/v1",
		"token",
		"host-1",
		"/bin/sh",
		80,
		24,
		"/tmp/lingon-harness/.lingon/tls",
		stdinFile,
		stdoutFile,
		false,
		&ptyMu,
		&ptyBuf,
	)
	if !opts.DisableDesktopNotifications {
		t.Fatal("expected android harness hosts to disable desktop notifications")
	}
	if opts.TLSDir != "/tmp/lingon-harness/.lingon/tls" {
		t.Fatalf("TLSDir = %q, want harness TLS dir", opts.TLSDir)
	}
}

func TestWriteHostScriptDoesNotTouchCallerTTY(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	scriptPath, err := writeHostScript(dir, "host-1", "/tmp/lingon-android-harness")
	if err != nil {
		t.Fatalf("writeHostScript: %v", err)
	}
	if filepath.Dir(scriptPath) != dir {
		t.Fatalf("script path dir = %q, want %q", filepath.Dir(scriptPath), dir)
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "stty ") {
		t.Fatalf("expected host launcher script not to mutate tty settings, got:\n%s", content)
	}
	if strings.Contains(content, "/dev/tty") {
		t.Fatalf("expected host launcher script not to reference caller tty, got:\n%s", content)
	}
	if !strings.Contains(content, `exec "/tmp/lingon-android-harness" -host-echo -host-id "host-1"`) {
		t.Fatalf("expected host launcher script to exec harness host echo, got:\n%s", content)
	}
}

func TestHarnessStopRemovesTempRoot(t *testing.T) {
	t.Setenv("LINGON_CONFIG_DIR", "")
	t.Setenv("LINGON_TERMINAL_DISABLE_DESKTOP_NOTIFICATIONS", "")
	t.Setenv("LINGON_DEBUG_INPUT", "")
	t.Setenv("LINGON_HOST_PUBLISHER_PING_INTERVAL", "")
	t.Setenv("LINGON_HOST_PUBLISHER_PING_TIMEOUT", "")
	t.Setenv("LINGON_HOST_ECHO_LOG", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := startHarness(ctx, harnessOptions{
		sessionCount: 0,
		port:         0,
		basePath:     "/v1",
		username:     "testuser",
		password:     "testpass",
		cols:         80,
		rows:         24,
	})
	if err != nil {
		t.Fatalf("startHarness: %v", err)
	}

	baseDir := h.baseDir
	if filepath.Base(baseDir) == "" || !strings.HasPrefix(filepath.Base(baseDir), "lingon-android-harness-") {
		t.Fatalf("baseDir = %q, want lingon-android-harness temp dir", baseDir)
	}
	if _, err := os.Stat(baseDir); err != nil {
		t.Fatalf("expected harness temp dir to exist before stop: %v", err)
	}
	if !strings.HasPrefix(h.config.HostEchoLog, baseDir+string(os.PathSeparator)) {
		t.Fatalf("host echo log = %q, want path under %q", h.config.HostEchoLog, baseDir)
	}

	h.stop()
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		t.Fatalf("expected harness temp dir to be removed after stop, stat err=%v", err)
	}
}
