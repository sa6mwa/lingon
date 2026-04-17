package session_test

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostSIGWINCHOnlyChangesViewportNotLocalPTY(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-local-host",
		SessionName: "viewport-local-host",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	assertPTYSize(t, host, 120, 40)

	host.Send("echo LOCAL_READY\n")
	waitForRawContains(t, host, "LOCAL_READY", 2*time.Second, 50*time.Millisecond, "expected local shell readiness marker")

	host.Send("stty size; echo LOCAL_SIZE_DONE\n")
	if !screenContainsWithin(host, "LOCAL_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected initial local PTY size marker on screen")
	}
	if !screenContainsWithin(host, "40 120", 2*time.Second) {
		t.Fatalf("expected initial local PTY size on screen, got:\n%s", host.Screen().String())
	}

	host.Resize(80, 24)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
	advanceTestClock(host.Clock(), 200*time.Millisecond)
	assertPTYSize(t, host, 80, 24)

	host.Send("stty size; echo LOCAL_SIZE_AFTER_DONE\n")
	if !screenContainsWithin(host, "LOCAL_SIZE_AFTER_DONE", 2*time.Second) {
		t.Fatalf("expected viewport resize marker on screen")
	}
	if !screenContainsWithin(host, "40 120", 2*time.Second) {
		t.Fatalf("expected viewport resize to leave local PTY size unchanged, got:\n%s", host.Screen().String())
	}
	if strings.Contains(host.Screen().String(), "24 80") {
		t.Fatalf("viewport SIGWINCH leaked into local PTY size; screen:\n%s", host.Screen().String())
	}
}

func TestRemoteTabActivationKeepsRemotePTYSize(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-remote-source",
		SessionName: "viewport-remote-source",
		Shell:       shell,
		Cols:        120,
		Rows:        40,
	})
	t.Cleanup(hostA.Cancel)

	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "viewport-remote-viewer",
		SessionName: "viewport-remote-viewer",
		Shell:       shell,
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)
	assertPTYSize(t, hostA, 120, 40)
	assertPTYSize(t, hostB, 80, 24)

	hostA.Send("echo SOURCE_READY\n")
	waitForRawContains(t, hostA, "SOURCE_READY", 2*time.Second, 50*time.Millisecond, "expected source shell readiness marker")

	hostA.Send("stty size; echo SOURCE_SIZE_DONE\n")
	if !screenContainsWithin(hostA, "SOURCE_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected initial source PTY size marker on source host")
	}
	if !screenContainsWithin(hostA, "40 120", 2*time.Second) {
		t.Fatalf("expected initial source PTY size on source host, got:\n%s", hostA.Screen().String())
	}

	reachedRemote := false
	for i := 0; i < 4; i++ {
		token := "REMOTE_VIEW_ACTIVE"
		hostB.SendCtrlL()
		hostB.Send("n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		hostB.Send("echo " + token + "\n")
		advanceTestClock(hostB.Clock(), 200*time.Millisecond)
		if screenContainsWithin(hostA, token, 1500*time.Millisecond) {
			reachedRemote = true
			break
		}
	}
	if !reachedRemote {
		t.Fatalf("timed out switching viewer host to remote tab")
	}

	hostB.Send("stty size; echo REMOTE_SIZE_DONE\n")
	if !screenContainsWithin(hostA, "REMOTE_SIZE_DONE", 2*time.Second) {
		t.Fatalf("expected remote size marker on source host")
	}
	if !screenContainsWithin(hostA, "40 120", 2*time.Second) {
		t.Fatalf("expected remote tab activation to preserve source PTY size, got:\n%s", hostA.Screen().String())
	}
	if strings.Contains(hostA.Screen().String(), "24 80") {
		t.Fatalf("remote viewer viewport leaked into source PTY size; screen:\n%s", hostA.Screen().String())
	}
	assertPTYSize(t, hostA, 120, 40)
	assertPTYSize(t, hostB, 80, 24)
}

func assertPTYSize(t *testing.T, sess *ptytest.PTYSession, wantCols, wantRows int) {
	t.Helper()
	cols, rows := sess.TTYSize()
	if cols != wantCols || rows != wantRows {
		t.Fatalf("pty size = %dx%d, want %dx%d", cols, rows, wantCols, wantRows)
	}
}
