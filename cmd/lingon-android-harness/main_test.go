package main

import (
	"bytes"
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
		stdinFile,
		stdoutFile,
		false,
		&ptyMu,
		&ptyBuf,
	)
	if !opts.DisableDesktopNotifications {
		t.Fatal("expected android harness hosts to disable desktop notifications")
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
