package session_test

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestHostRemoteTabSwitchAfterRelayDropWithInactiveTTL(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)

	mockClock := clock.NewMock()

	hostA := startHostWithClock(t, h, session.Options{
		Endpoint:    h.Endpoint(),
		Token:       h.AccessToken(),
		SessionID:   "freezeA",
		SessionName: "freezeA",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
		Publish:     true,
		Clock:       mockClock,
	})
	hostB := startHostWithClock(t, h, session.Options{
		Endpoint:    h.Endpoint(),
		Token:       h.AccessToken(),
		SessionID:   "freezeB",
		SessionName: "freezeB",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
		Publish:     true,
		Clock:       mockClock,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 5*time.Second)

	hostA.SendCtrlL()
	hostA.Send("c")
	hostB.SendCtrlL()
	hostB.Send("c")

	waitForSessionCountWithClock(t, h.Endpoint(), h.AccessToken(), 4, 12*time.Second, mockClock)

	hostB.Send("sleep 5\n")
	primeTabsByCountSession(t, hostA, 4)

	h.StopServer()
	h.RestartServer()
	waitForSessionCountWithClock(t, h.Endpoint(), h.AccessToken(), 4, 12*time.Second, mockClock)
	mockClock.Add(45 * time.Second)
	mockClock.Add(500 * time.Millisecond)

	hostA.SendCtrlL()
	hostA.Send("b")
	hostA.Wait(150 * time.Millisecond)
	remoteReached := false
	for i := 0; i < 4; i++ {
		hostA.SendCtrlL()
		hostA.Send("n")
		hostA.Send("echo REMOTE_AFTER\n")
		advanceTestClock(hostA.Clock(), 200*time.Millisecond)
		if screenContainsWithin(hostB, "REMOTE_AFTER", 500*time.Millisecond) {
			remoteReached = true
			break
		}
	}
	if !remoteReached {
		t.Fatalf("remote tab switch did not deliver input after inactive TTL")
	}
}

func waitForSessionCountWithClock(t *testing.T, endpoint, token string, want int, timeout time.Duration, clk clock.Clock) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		found, err := fetchSessionIDsSession(endpoint, token)
		if err == nil && len(found) == want {
			return
		}
		advanceTestClock(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d sessions", want)
}

func startHostWithClock(t *testing.T, h *ptytest.Harness, opts session.Options) *ptytest.PTYSession {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, opts.Cols, opts.Rows)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, opts.Cols, opts.Rows, opts.Clock)
	if opts.Stdin == nil {
		opts.Stdin = slave
	}
	if opts.Stdout == nil {
		opts.Stdout = slave
	}
	go func() {
		sess.SetRunErr(session.New(opts).Run(sess.Context()))
	}()
	return sess
}
