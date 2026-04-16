package main

import (
	"bytes"
	"os"
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
