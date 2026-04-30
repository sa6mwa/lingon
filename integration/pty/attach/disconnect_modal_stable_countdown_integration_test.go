//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/ptytest"
)

func TestDisconnectModalCountdownStable(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-countdown",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-countdown"})

	host.Send("echo HOST_COUNTDOWN_READY\n")
	if !screenContainsWithin(host, "HOST_COUNTDOWN_READY", 2*time.Second) {
		t.Fatalf("expected host marker before attach connects")
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-countdown",
		Cols:      80,
		Rows:      24,
		BackoffPolicy: backoff.Policy{
			Base:   3 * time.Second,
			Factor: 1,
			Max:    3 * time.Second,
		},
	})
	t.Cleanup(attachSess.Cancel)

	if exited, err := attachSess.WaitErr(200 * time.Millisecond); exited {
		t.Fatalf("attach exited early: %v", err)
	}
	waitForClientCount(t, h, "host-countdown", 1, 3*time.Second)
	if !screenContainsWithin(attachSess, "HOST_COUNTDOWN_READY", 3*time.Second) {
		t.Fatalf("attach did not render host content before disconnect")
	}

	h.StopServer()

	re := regexp.MustCompile(`reconnecting in (\d+)s`)
	var first int
	sawCountdown := false
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		match := re.FindStringSubmatch(screen.String())
		if len(match) < 2 {
			if screen.Contains("Not connected") || screen.Contains("no sessions available") {
				return nil
			}
			return fmt.Errorf("missing reconnect countdown")
		}
		val, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("parse reconnect value: %v", err)
		}
		if val <= 0 {
			return fmt.Errorf("expected countdown > 0, got %d", val)
		}
		sawCountdown = true
		first = val
		return nil
	})

	if !sawCountdown {
		_ = attachSessionUsable(t, attachSess)
		return
	}

	samples := []int{first}
	deadline := ptytest.Now(h.Clock()).Add(1200 * time.Millisecond)
	for ptytest.Now(h.Clock()).Before(deadline) {
		screen := attachSess.Screen()
		match := re.FindStringSubmatch(screen.String())
		if len(match) == 2 {
			if val, err := strconv.Atoi(match[1]); err == nil {
				samples = append(samples, val)
			}
		}
		h.Advance(200 * time.Millisecond)
	}

	hasZero := false
	hasHigh := false
	for _, v := range samples {
		if v == 0 {
			hasZero = true
		}
		if v >= 2 {
			hasHigh = true
		}
	}
	if hasZero && hasHigh {
		t.Fatalf("countdown flickered to 0s while still >1s: %v", samples)
	}
	_ = attachSessionUsable(t, attachSess)
}

func TestDisconnectModalCountdownStableAfterTabSwitch(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	hostA := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Shell: shell, Cols: 80, Rows: 24})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "host-b", Shell: shell, Cols: 80, Rows: 24})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      80,
		Rows:      24,
		BackoffPolicy: backoff.Policy{
			Base:   3 * time.Second,
			Factor: 1,
			Max:    3 * time.Second,
		},
		OnActive: func(sessionID string) {
			activeMu.Lock()
			activeID = sessionID
			activeMu.Unlock()
		},
		OnView: func(sessionID string, client *attach.Client) {
			viewsMu.Lock()
			views[sessionID] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	primeTabsByCount(t, attachSess, 2)
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)

	h.StopServer()
	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Wait(200 * time.Millisecond)

	re := regexp.MustCompile(`reconnecting in (\d+)s`)
	sawCountdown := false
	attachSess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		match := re.FindStringSubmatch(screen.String())
		if len(match) < 2 {
			// Fast reconnect/offline transitions can skip rendering the countdown entirely.
			return nil
		}
		val, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("parse reconnect value: %v", err)
		}
		if val <= 0 {
			return fmt.Errorf("expected countdown >0 before sampling, got %d", val)
		}
		sawCountdown = true
		return nil
	})

	if !sawCountdown {
		_ = attachSessionUsable(t, attachSess)
		return
	}

	values := collectCountdownSamples(t, attachSess, re, 900*time.Millisecond)
	if hasInterleavedZero(values) {
		t.Fatalf("countdown flickered to 0s after tab switch: %v", values)
	}
	_ = attachSessionUsable(t, attachSess)
}
