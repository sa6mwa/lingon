//go:build integration
// +build integration

package integrationptyattach_test

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relay"
)

func TestAttachViewOnlyIgnoresInputAndShowsBanner(t *testing.T) {
	h := newHarness(t)

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "view-only",
		SessionName: "view-only",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"view-only"})

	token := h.CreateShareToken("view-only", relay.ShareScopeView, time.Minute)
	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:  "view-only",
		ShareToken: token,
		Cols:       80,
		Rows:       24,
	})
	t.Cleanup(attach.Cancel)

	host.Send("echo READY\r\n")
	waitUntilDebug(t, h.Clock(), 3*time.Second, func() bool {
		return attach.Screen().Contains("READY")
	}, func() string {
		return attach.Screen().String()
	})

	attach.SendBytes([]byte("x"))
	waitUntilDebug(t, h.Clock(), 3*time.Second, func() bool {
		return attach.Screen().Contains("control not permitted")
	}, func() string {
		return attach.Screen().String()
	})

	if done, err := attach.WaitErr(200 * time.Millisecond); done {
		t.Fatalf("attach exited after view-only input: %v", err)
	}

	h.Advance(4 * time.Second)
	waitUntilDebug(t, h.Clock(), 3*time.Second, func() bool {
		return !attach.Screen().Contains("control not permitted")
	}, func() string {
		return attach.Screen().String()
	})

	host.Send("echo STILL\r\n")
	waitUntilDebug(t, h.Clock(), 3*time.Second, func() bool {
		return attach.Screen().Contains("STILL")
	}, func() string {
		return attach.Screen().String()
	})
}
