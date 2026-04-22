//go:build integration
// +build integration

package integrationptysession_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRemoteTabSwitchAfterRelayDrop(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "hostA",
		SessionName: "hostA",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "hostB",
		SessionName: "hostB",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 4, 6*time.Second)

	token := "REMOTE_B_TOKEN"
	hostB.Send("echo " + token + "\n")
	if !screenContainsWithin(hostB, token, 2*time.Second) {
		t.Fatalf("expected token on hostB")
	}

	if !switchToToken(hostA, token, 4, 600*time.Millisecond) {
		hostB.Send("echo " + token + "\n")
		if !switchToToken(hostA, token, 4, 600*time.Millisecond) {
			t.Fatalf("unable to switch to token %q", token)
		}
	}

	hostA.DrainRaw()
	traceOffset := traceSize(t, h.TracePath())
	h.StopServer()
	h.Advance(300 * time.Millisecond)

	hostA.SendCtrlL()
	hostA.Send("n")
	if !tabSwitchEventWithin(hostA, h.TracePath(), traceOffset, 2*time.Second) {
		beforeCursor := hostA.Cursor()
		hostA.Send("\n")
		advanceTestClock(h.Clock(), 200*time.Millisecond)
		afterCursor := hostA.Cursor()
		t.Fatalf("tab bar did not update while offline; cursor_before=%+v cursor_after=%+v trace=%s", beforeCursor, afterCursor, h.TracePath())
	}

	h.RestartServer()
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 4, 6*time.Second)

	hostA.DrainRaw()
	traceOffset = traceSize(t, h.TracePath())
	hostA.SendCtrlL()
	hostA.Send("n")
	if !tabSwitchEventWithin(hostA, h.TracePath(), traceOffset, 2*time.Second) {
		beforeCursor := hostA.Cursor()
		hostA.Send("\n")
		advanceTestClock(h.Clock(), 200*time.Millisecond)
		afterCursor := hostA.Cursor()
		t.Fatalf("tab bar did not update after reconnect; cursor_before=%+v cursor_after=%+v trace=%s", beforeCursor, afterCursor, h.TracePath())
	}
}

func screenContainsWithin(sess *ptytest.PTYSession, token string, timeout time.Duration) bool {
	deadline := sess.Clock().Now().Add(timeout)
	for sess.Clock().Now().Before(deadline) {
		if sess.Screen().Contains(token) {
			return true
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
	return false
}

func traceSize(t *testing.T, tracePath string) int64 {
	t.Helper()
	info, err := os.Stat(tracePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func tabSwitchEventWithin(sess *ptytest.PTYSession, tracePath string, startOffset int64, timeout time.Duration) bool {
	deadline := sess.Clock().Now().Add(timeout)
	for sess.Clock().Now().Before(deadline) {
		if hasTabSwitchEventSince(tracePath, startOffset) {
			return true
		}
		advanceTestClock(sess.Clock(), 50*time.Millisecond)
	}
	return false
}

func hasTabSwitchEventSince(tracePath string, startOffset int64) bool {
	data, err := os.ReadFile(tracePath)
	if err != nil || len(data) == 0 {
		return false
	}
	if startOffset < 0 {
		startOffset = 0
	}
	if startOffset > int64(len(data)) {
		return false
	}
	lines := strings.Split(string(data[startOffset:]), "\n")
	for _, line := range lines {
		if !strings.Contains(line, `"event":"tab_switch"`) {
			continue
		}
		var event struct {
			Event     string `json:"event"`
			CurrentID string `json:"current_id"`
			NextID    string `json:"next_id"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Event == "tab_switch" && event.NextID != "" && event.NextID != event.CurrentID {
			return true
		}
	}
	return false
}

func switchToToken(sess *ptytest.PTYSession, token string, tabs int, wait time.Duration) bool {
	for i := 0; i < tabs+1; i++ {
		if screenContainsWithin(sess, token, wait) {
			return true
		}
		sess.SendCtrlL()
		sess.Send("n")
		advanceTestClock(sess.Clock(), 150*time.Millisecond)
	}
	return false
}
