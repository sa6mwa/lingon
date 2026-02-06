package attach_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachMultiHostRelayDropSwitchesTabs(t *testing.T) {
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

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 6*time.Second)

	attachA, activeA, viewsA := startTrackedAttach(t, h, "hostA")
	attachB, activeB, viewsB := startTrackedAttach(t, h, "hostB")
	t.Cleanup(attachA.Cancel)
	t.Cleanup(attachB.Cancel)

	primeTabsByCountWithActive(t, attachA, 4, h.Clock(), activeA)
	primeTabsByCountWithActive(t, attachB, 4, h.Clock(), activeB)

	h.StopServer()
	h.Advance(300 * time.Millisecond)

	attachA.SendCtrlL()
	attachA.Send("n")
	attachB.SendCtrlL()
	attachB.Send("n")
	h.Advance(200 * time.Millisecond)

	h.RestartServer()
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 6*time.Second)
	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	for id := range ids {
		waitForClientCount(t, h, id, 1, 6*time.Second)
	}
	primeTabsByCountWithActive(t, attachA, 4, h.Clock(), activeA)
	primeTabsByCountWithActive(t, attachB, 4, h.Clock(), activeB)

	tokensA := cycleSendTokensWithActive(t, attachA, 1, "RELAY_DROP_A", h.Clock(), activeA.mu, activeA.id, activeA.viewsMu, viewsA)
	tokensB := cycleSendTokensWithActive(t, attachB, 1, "RELAY_DROP_B", h.Clock(), activeB.mu, activeB.id, activeB.viewsMu, viewsB)
	assertTokensVisibleAcrossTabs(t, attachA, 4, tokensA, "attachA")
	assertTokensVisibleAcrossTabs(t, attachB, 4, tokensB, "attachB")
}
