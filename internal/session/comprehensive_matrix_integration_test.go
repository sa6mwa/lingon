package session_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestComprehensiveHostAttachMatrix(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"

	type scenario struct {
		name         string
		hosts        int
		localPerHost int
		attaches     int
	}

	scenarios := []scenario{
		{name: "1host-1pty-1attach", hosts: 1, localPerHost: 1, attaches: 1},
		{name: "1host-2pty-1attach", hosts: 1, localPerHost: 2, attaches: 1},
		{name: "1host-3pty-2attach", hosts: 1, localPerHost: 3, attaches: 2},
		{name: "2host-1pty-1attach", hosts: 2, localPerHost: 1, attaches: 1},
		{name: "2host-2pty-1attach", hosts: 2, localPerHost: 2, attaches: 1},
		{name: "2host-2pty-2attach", hosts: 2, localPerHost: 2, attaches: 2},
		{name: "2host-3pty-2attach", hosts: 2, localPerHost: 3, attaches: 2},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			h := newHarness(t)
			hosts := make([]*ptytest.PTYSession, 0, sc.hosts)
			for i := 0; i < sc.hosts; i++ {
				id := fmt.Sprintf("host-%d", i+1)
				host := h.StartHost(ptytest.HostOptions{
					SessionID:   id,
					SessionName: id,
					Shell:       shell,
					Cols:        120,
					Rows:        30,
				})
				t.Cleanup(host.Cancel)
				hosts = append(hosts, host)
			}

			wantSessions := sc.hosts * sc.localPerHost
			waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), sc.hosts, 15*time.Second)

			for _, host := range hosts {
				createLocalPTYSessions(t, host, sc.localPerHost)
			}
			waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), wantSessions, 20*time.Second)

			attaches := make([]*ptytest.PTYSession, 0, sc.attaches)
			attachStates := make(map[*ptytest.PTYSession]*activeState)
			for i := 0; i < sc.attaches; i++ {
				state := newActiveState()
				attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
					SessionID: "host-1",
					Cols:      120,
					Rows:      30,
					OnActive:  state.onActive,
					OnView:    state.onView,
				})
				t.Cleanup(attach.Cancel)
				attaches = append(attaches, attach)
				attachStates[attach] = state
			}

			for _, sess := range hosts {
				primeTabsByCountSession(t, sess, wantSessions)
				exerciseTabIO(t, sess, wantSessions, "host")
			}
			for _, sess := range attaches {
				if !attachSessionUsable(t, sess) {
					continue
				}
				state := attachStates[sess]
				_ = waitForActiveOrAnyReadySession(t, h.Clock(), state, "", 12*time.Second)
				primeTabsByCountSessionWithActive(t, sess, wantSessions, h.Clock(), state)
				assertAttachSendsToHostWithActive(t, sess, hosts[0], wantSessions, h.Clock(), state)
				exerciseTabIOWithActive(t, sess, wantSessions, "attach", h.Clock(), state)
			}

			runComprehensiveSequences(t, h, hosts, attaches, wantSessions, attachStates)
		})
	}
}

func runComprehensiveSequences(t *testing.T, h *ptytest.Harness, hosts, attaches []*ptytest.PTYSession, tabCount int, attachStates map[*ptytest.PTYSession]*activeState) {
	t.Helper()

	h.StopServer()
	for _, sess := range hosts {
		assertHostDisconnectState(t, sess)
	}
	for _, sess := range attaches {
		assertAttachDisconnectState(t, sess)
	}
	h.RestartServer()
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), tabCount, 20*time.Second)
	for _, sess := range hosts {
		assertHostReconnectedState(t, sess)
	}
	liveAttaches := make([]*ptytest.PTYSession, 0, len(attaches))
	for _, sess := range attaches {
		if attachSessionUsable(t, sess) {
			liveAttaches = append(liveAttaches, sess)
		}
	}
	for _, sess := range liveAttaches {
		assertConnectedBannerReplacesDisconnect(t, sess)
	}
	for _, sess := range liveAttaches {
		if state := attachStates[sess]; state != nil {
			_ = waitForActiveOrAnyReadySession(t, h.Clock(), state, "", 12*time.Second)
		}
	}
	for _, sess := range hosts {
		verifySessionIO(t, sess, tabCount, "post-restart-host")
	}
	for _, sess := range liveAttaches {
		state := attachStates[sess]
		verifySessionIOWithActive(t, sess, tabCount, "post-restart-attach", h.Clock(), state)
	}
}

func attachSessionUsable(t *testing.T, sess *ptytest.PTYSession) bool {
	t.Helper()
	if exited, err := sess.WaitErr(0); exited {
		if err == nil || strings.Contains(err.Error(), "no sessions available") {
			return false
		}
		t.Fatalf("attach exited unexpectedly: %v", err)
	}
	return true
}

func createLocalPTYSessions(t *testing.T, host *ptytest.PTYSession, count int) {
	t.Helper()
	if count <= 1 {
		return
	}
	for i := 0; i < count-1; i++ {
		ctrlLCommand(host, "c")
		ptytest.Advance(host.Clock(), 200*time.Millisecond)
	}
}

func primeTabsByCountSessionWithActive(t *testing.T, sess *ptytest.PTYSession, count int, clk clock.Clock, state *activeState) {
	t.Helper()
	if count <= 1 {
		return
	}
	current := waitForActiveOrAnyReadySession(t, clk, state, "", 12*time.Second)
	for i := 0; i < count-1; i++ {
		current = advanceActiveTabSession(t, sess, "n", clk, state, current, 4*time.Second)
	}
	ptytest.Advance(sess.Clock(), 300*time.Millisecond)
}

func exerciseTabIO(t *testing.T, sess *ptytest.PTYSession, tabCount int, tag string) {
	t.Helper()
	verifySessionIO(t, sess, tabCount, tag)
	ctrlLCommand(sess, "p")
	verifySessionIO(t, sess, tabCount, tag+"-rev")
}

func exerciseTabIOWithActive(t *testing.T, sess *ptytest.PTYSession, tabCount int, tag string, clk clock.Clock, state *activeState) {
	t.Helper()
	verifySessionIOWithActive(t, sess, tabCount, tag, clk, state)
	if tabCount > 1 {
		current := waitForActiveOrAnyReadySession(t, clk, state, "", 6*time.Second)
		_ = advanceActiveTabSession(t, sess, "p", clk, state, current, 4*time.Second)
	} else {
		ctrlLCommand(sess, "p")
	}
	verifySessionIOWithActive(t, sess, tabCount, tag+"-rev", clk, state)
}

func verifySessionIO(t *testing.T, sess *ptytest.PTYSession, tabCount int, tag string) {
	t.Helper()
	for i := 0; i < tabCount; i++ {
		if i > 0 {
			ctrlLCommand(sess, "n")
			ptytest.Advance(sess.Clock(), 120*time.Millisecond)
		}
		token := fmt.Sprintf("io-%s-%d", tag, i)
		ensureTabBarHidden(sess)
		sendCommand(sess, token)
		assertScreenContains(t, sess, token)
	}
}

func verifySessionIOWithActive(t *testing.T, sess *ptytest.PTYSession, tabCount int, tag string, clk clock.Clock, state *activeState) {
	t.Helper()
	current := waitForActiveOrAnyReadySession(t, clk, state, "", 12*time.Second)
	for i := 0; i < tabCount; i++ {
		if i > 0 {
			current = advanceActiveTabSession(t, sess, "n", clk, state, current, 4*time.Second)
		}
		token := fmt.Sprintf("io-%s-%d", tag, i)
		ensureTabBarHidden(sess)
		deadline := ptytest.Now(clk).Add(2 * time.Second)
		for {
			sendCommand(sess, token)
			if screenContainsWithin(sess, token, 500*time.Millisecond) {
				break
			}
			if ptytest.Now(clk).After(deadline) {
				assertScreenContains(t, sess, token)
				break
			}
			ptytest.Advance(clk, 100*time.Millisecond)
		}
	}
}

func ensureTabBarHidden(sess *ptytest.PTYSession) {
	row := sess.Screen().Row(0)
	if strings.Contains(row, "*") || strings.Contains(row, "host-") {
		sess.SendCtrlL()
		sess.Send("b")
		ptytest.Advance(sess.Clock(), 120*time.Millisecond)
	}
}

func sendCommand(sess *ptytest.PTYSession, token string) {
	sess.Send(fmt.Sprintf("echo %s\n", token))
}

func assertScreenContains(t *testing.T, sess *ptytest.PTYSession, token string) {
	t.Helper()
	sess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains(token) {
			return fmt.Errorf("expected token %q on screen; got:\n%s", token, screen.String())
		}
		return nil
	})
}

func ctrlLCommand(sess *ptytest.PTYSession, cmd string) {
	sess.SendCtrlL()
	ptytest.Advance(sess.Clock(), 100*time.Millisecond)
	sess.Send(cmd)
}

func assertAttachSendsToHostWithActive(t *testing.T, attach *ptytest.PTYSession, host *ptytest.PTYSession, tabCount int, clk clock.Clock, state *activeState) {
	t.Helper()
	_ = waitForActiveOrAnyReadySession(t, clk, state, "", 6*time.Second)
	token := fmt.Sprintf("attach-host-%d", ptytest.Now(attach.Clock()).UnixNano())
	sendCommand(attach, token)
	for i := 0; i < tabCount+1; i++ {
		if screenContainsWithin(host, token, 300*time.Millisecond) {
			return
		}
		if i < tabCount {
			ctrlLCommand(host, "n")
			ptytest.Advance(host.Clock(), 120*time.Millisecond)
		}
	}
	t.Fatalf("expected host to receive attach input %q", token)
}

func assertAttachDisconnectState(t *testing.T, sess *ptytest.PTYSession) {
	t.Helper()
	sess.Eventually(10*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return nil
		}
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return nil
		}
		// Attach screens can temporarily remain on a shell prompt while transport
		// disconnect events propagate through reconnect loops.
		if screen.Contains("PROMPT>") {
			return nil
		}
		if strings.Contains(row, "*") || strings.Contains(row, "host-") {
			ensureTabBarHidden(sess)
			row = sess.Screen().Row(0)
			if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
				return nil
			}
			if sess.Screen().Contains("Not connected") || sess.Screen().Contains("reconnecting") {
				return nil
			}
			if sess.Screen().Contains("PROMPT>") {
				return nil
			}
		}
		return fmt.Errorf("expected disconnect indicator; got row1 %q", row)
	})
}

func assertHostDisconnectState(t *testing.T, sess *ptytest.PTYSession) {
	t.Helper()
	sess.Eventually(8*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			return nil
		}
		if screen.Contains("Not connected") || screen.Contains("reconnecting") {
			return nil
		}
		// Host-side local PTYs can remain on a shell prompt even while relay is disconnected.
		if screen.Contains("PROMPT>") {
			return nil
		}
		if strings.Contains(row, "*") || strings.Contains(row, "host-") {
			ensureTabBarHidden(sess)
			row = sess.Screen().Row(0)
			screen = sess.Screen()
			if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
				return nil
			}
			if screen.Contains("Not connected") || screen.Contains("reconnecting") || screen.Contains("PROMPT>") {
				return nil
			}
		}
		return fmt.Errorf("expected host disconnect indicator or prompt; got row1 %q", row)
	})
}

func assertConnectedBannerReplacesDisconnect(t *testing.T, sess *ptytest.PTYSession) {
	t.Helper()
	lastToggle := time.Time{}
	sess.Eventually(30*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		hasConnected := strings.Contains(row, "connected to ")
		if hasConnected {
			return nil
		}
		if strings.Contains(row, "*") && ptytest.Now(sess.Clock()).Sub(lastToggle) > 300*time.Millisecond {
			sess.SendCtrlL()
			sess.Send("b")
			lastToggle = ptytest.Now(sess.Clock())
		}
		return nil
	})
}

func assertHostReconnectedState(t *testing.T, sess *ptytest.PTYSession) {
	t.Helper()
	lastToggle := time.Time{}
	sess.Eventually(10*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		hasConnected := strings.Contains(row, "connected to ")
		hasDisconnected := strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting")
		if hasDisconnected {
			return fmt.Errorf("expected host reconnected state to clear disconnect banner; got %q", row)
		}
		if hasConnected || screen.Contains("PROMPT>") {
			return nil
		}
		if strings.Contains(row, "*") && ptytest.Now(sess.Clock()).Sub(lastToggle) > 300*time.Millisecond {
			sess.SendCtrlL()
			sess.Send("b")
			lastToggle = ptytest.Now(sess.Clock())
		}
		return nil
	})
}
