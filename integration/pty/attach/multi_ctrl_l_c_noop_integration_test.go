//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachCtrlLCIsNoOp(t *testing.T) {
	h := newHarness(t)

	host := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Cols: 120, Rows: 30})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-a"})

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	host.Send("echo ATTACH_CTRL_L_C_READY\n")
	attachSess.Eventually(5*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("ATTACH_CTRL_L_C_READY") {
			return fmt.Errorf("expected attach stream to be live before ctrl+l c")
		}
		return nil
	})

	before, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before ctrl+l c: %v", err)
	}

	attachSess.SendCtrlL()
	attachSess.Send("c")
	attachSess.Wait(500 * time.Millisecond)

	after, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions after ctrl+l c: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("ctrl+l c unexpectedly changed session count: before=%d after=%d", len(before), len(after))
	}
	for id := range before {
		if !after[id] {
			t.Fatalf("ctrl+l c unexpectedly removed session %q", id)
		}
	}

	host.Send("echo CTRL_L_C_NOOP_OK\n")
	attachSess.Eventually(5*time.Second, 80*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("CTRL_L_C_NOOP_OK") {
			return fmt.Errorf("expected attach stream alive after ctrl+l c no-op")
		}
		return nil
	})
}
