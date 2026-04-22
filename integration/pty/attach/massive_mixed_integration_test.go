//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMassiveMixedMultiHostMultiAttachIO(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID: "host-a",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID: "host-b",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 4, 5*time.Second)
	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	sessionCount := len(ids)
	if sessionCount < 4 {
		t.Fatalf("expected >=4 sessions, got %d", sessionCount)
	}

	primeTabsByCount(t, hostA, sessionCount)
	primeTabsByCount(t, hostB, sessionCount)

	var activeMuA sync.Mutex
	activeA := ""
	var viewsMuA sync.Mutex
	viewsA := make(map[string]*attach.Client)
	attachA := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMuA.Lock()
			activeA = id
			activeMuA.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMuA.Lock()
			viewsA[id] = client
			viewsMuA.Unlock()
		},
	})

	var activeMuB sync.Mutex
	activeB := ""
	var viewsMuB sync.Mutex
	viewsB := make(map[string]*attach.Client)
	attachB := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-b",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMuB.Lock()
			activeB = id
			activeMuB.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMuB.Lock()
			viewsB[id] = client
			viewsMuB.Unlock()
		},
	})
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuA, &activeA, &viewsMuA, viewsA, "", 3*time.Second)
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMuB, &activeB, &viewsMuB, viewsB, "", 3*time.Second)
	primeTabsByCount(t, attachA, sessionCount)
	primeTabsByCount(t, attachB, sessionCount)

	attachATokens := cycleSendTokens(t, attachA, 1, "ATTACHA")
	assertTokensVisibleAcrossTabs(t, hostA, sessionCount, attachATokens, "hostA")
	assertTokensVisibleAcrossTabs(t, hostB, sessionCount, attachATokens, "hostB")

	attachBTokens := cycleSendTokens(t, attachB, 1, "ATTACHB")
	assertTokensVisibleAcrossTabs(t, hostA, sessionCount, attachBTokens, "hostA")
	assertTokensVisibleAcrossTabs(t, hostB, sessionCount, attachBTokens, "hostB")
}
