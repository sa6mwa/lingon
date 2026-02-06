package attach_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRemoteTabSwitchRendersWithoutExtraInput(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-switch-a",
		SessionName: "sw-a",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-switch-b",
		SessionName: "sw-b",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-switch-a", "attach-switch-b"})

	tokenA := "ATTACH_SWITCH_TOKEN_A_991"
	tokenB := "ATTACH_SWITCH_TOKEN_B_427"
	hostA.Send("clear; i=1; while [ $i -le 20 ]; do echo " + tokenA + "_$i; i=$((i+1)); done\n")
	if !screenContainsWithin(hostA, tokenA, 2*time.Second) {
		t.Fatalf("expected token %q on host A", tokenA)
	}
	hostB.Send("clear; i=1; while [ $i -le 20 ]; do echo " + tokenB + "_$i; i=$((i+1)); done\n")
	if !screenContainsWithin(hostB, tokenB, 2*time.Second) {
		t.Fatalf("expected token %q on host B", tokenB)
	}

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-switch-a",
		Cols:      100,
		Rows:      30,
		OnView: func(id string, c *attach.Client) {
			viewsMu.Lock()
			views[id] = c
			viewsMu.Unlock()
		},
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
	})
	t.Cleanup(attach.Cancel)

	waitForTabLabels(t, attach, []string{"sw-a", "sw-b"}, 6*time.Second)
	current := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	current = advanceActiveTabWithRetry(t, attach, h.Clock(), &activeMu, &activeID, &viewsMu, views, current, 3*time.Second)
	advanceActiveTabWithRetry(t, attach, h.Clock(), &activeMu, &activeID, &viewsMu, views, current, 3*time.Second)

	attach.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(tokenA) {
			return fmt.Errorf("expected token %q before final switch", tokenA)
		}
		return nil
	})

	waitForRawIdle(t, attach, 120*time.Millisecond, 2*time.Second)
	_ = attach.DrainRaw()

	// Switch to tab B and require immediate framebuffer swap without extra input.
	attach.SendCtrlL()
	attach.Send("n")
	attach.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(tokenB) {
			return fmt.Errorf("expected tab B token %q immediately after switch", tokenB)
		}
		if screen.Contains(tokenA) {
			return fmt.Errorf("unexpected stale tab A token %q after switch", tokenA)
		}
		return nil
	})
}
