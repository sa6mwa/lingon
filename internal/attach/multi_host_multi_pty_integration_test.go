package attach_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiHostMultiPTYInteraction(t *testing.T) {
	h := newHarness(t)

	hostA := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Cols: 120, Rows: 30})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-a"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}

	hostA.SendCtrlL()
	hostA.Send("c")
	newID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)

	hostB := h.StartHost(ptytest.HostOptions{SessionID: "host-b", Cols: 120, Rows: 30})
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)
	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	sessionCount := len(ids)
	if sessionCount != 3 {
		t.Fatalf("expected 3 sessions, got %d", sessionCount)
	}

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	primeTabsByCount(t, attachSess, sessionCount)
	primeTabsByCount(t, hostA, sessionCount)
	primeTabsByCount(t, hostB, sessionCount)

	for i := 0; i < 1; i++ {
		attachTokens := cycleSendTokensWithActive(t, attachSess, 1, fmt.Sprintf("ATTACH_%d", i), h.Clock(), &activeMu, &activeID, &viewsMu, views)
		_ = cycleSendTokens(t, hostA, 1, fmt.Sprintf("HOSTA_%d", i))
		_ = cycleSendTokens(t, hostB, 1, fmt.Sprintf("HOSTB_%d", i))

		assertTokensVisibleAcrossTabs(t, hostA, sessionCount, attachTokens, "host A")
		assertTokensVisibleAcrossTabs(t, hostB, sessionCount, attachTokens, "host B")

	}

	_ = newID
}

func TestMultiHostMultiPTYInteractionAfterCreate(t *testing.T) {
	h := newHarness(t)

	hostA := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Cols: 120, Rows: 30})
	hostB := h.StartHost(ptytest.HostOptions{SessionID: "host-b", Cols: 120, Rows: 30})
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 5*time.Second)

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})

	hostA.SendCtrlL()
	hostA.Send("c")
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)

	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	sessionCount := len(ids)
	if sessionCount != 3 {
		t.Fatalf("expected 3 sessions, got %d", sessionCount)
	}

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	primeTabsByCount(t, attachSess, sessionCount)
	primeTabsByCount(t, hostA, sessionCount)
	primeTabsByCount(t, hostB, sessionCount)

	for i := 0; i < 1; i++ {
		attachTokens := cycleSendTokensWithActive(t, attachSess, 1, fmt.Sprintf("ATTACH_LATE_%d", i), h.Clock(), &activeMu, &activeID, &viewsMu, views)
		_ = cycleSendTokens(t, hostB, 1, fmt.Sprintf("HOSTB_LATE_%d", i))

		assertTokensVisibleAcrossTabs(t, hostA, sessionCount, attachTokens, "host A")
		assertTokensVisibleAcrossTabs(t, hostB, sessionCount, attachTokens, "host B")
	}
}

func TestMultiHostMultiPTYRemoval(t *testing.T) {
	h := newHarness(t)

	hostA := h.StartHost(ptytest.HostOptions{SessionID: "host-a", Cols: 120, Rows: 30})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-a"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}

	hostA.SendCtrlL()
	hostA.Send("c")
	newID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)
	hostA.Send("echo EXIT_TARGET\n")
	if !screenContainsWithin(hostA, "EXIT_TARGET", 2*time.Second) {
		t.Fatalf("expected new host session to be active after ctrl+l c")
	}

	hostB := h.StartHost(ptytest.HostOptions{SessionID: "host-b", Cols: 120, Rows: 30})
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)
	ids, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	sessionCount := len(ids)

	labelMap := sessionLabelMap(t, h.Endpoint(), h.AccessToken())
	newLabel, ok := labelMap[newID]
	if !ok || newLabel == "" {
		t.Fatalf("missing label for new session %q", newID)
	}
	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-a",
		Cols:      120,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})
	switchAttachToHost(t, attachSess, hostB, sessionCount, h.Clock(), &activeMu, &activeID, &viewsMu, views)

	hostA.Send("exit\n")

	waitForSessionRemovalByID(t, h.Clock(), h.Endpoint(), h.AccessToken(), newID, 12*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("b")
	ptytest.Advance(attachSess.Clock(), 200*time.Millisecond)
	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, newLabel) {
			return fmt.Errorf("expected removed tab %q to disappear, row=%q", newLabel, row)
		}
		return nil
	})
}

func sessionLabelMap(t *testing.T, endpoint, token string) map[string]string {
	t.Helper()
	sessions, err := fetchSessions(endpoint, token)
	if err != nil {
		t.Fatalf("fetch sessions: %v", err)
	}
	labels := make(map[string]string, len(sessions))
	for _, session := range sessions {
		labels[session.ID] = sessionLabelForTest(session)
	}
	return labels
}

func labelsFromMap(labelMap map[string]string) []string {
	labels := make([]string, 0, len(labelMap))
	for _, label := range labelMap {
		labels = append(labels, label)
	}
	return labels
}

func sessionLabelForTest(session sessionRow) string {
	name := strings.TrimSpace(session.Name)
	if name != "" {
		return name
	}
	short := shortSessionIDForTest(session.ID)
	if short != "" {
		return short
	}
	return session.ID
}

func shortSessionIDForTest(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	parts := strings.Split(id, "-")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	if len(id) > 8 {
		return id[len(id)-8:]
	}
	return id
}

func waitForNewSessionIDFromSet(t *testing.T, clk clock.Clock, endpoint, token string, prior map[string]bool, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		current, err := fetchSessionIDs(endpoint, token)
		if err == nil {
			for id := range current {
				if !prior[id] {
					return id
				}
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for new session id")
	return ""
}

func waitForSessionRemovalByID(t *testing.T, clk clock.Clock, endpoint, token, id string, timeout time.Duration) {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		found, err := fetchSessionIDs(endpoint, token)
		if err == nil && !found[id] {
			return
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %q to be removed", id)
}

func primeTabsByCount(t *testing.T, sess *ptytest.PTYSession, count int) {
	t.Helper()
	for i := 0; i < count-1; i++ {
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 300*time.Millisecond)
	}
	ptytest.Advance(sess.Clock(), 500*time.Millisecond)
}

func cycleSendTokens(t *testing.T, sess *ptytest.PTYSession, count int, prefix string) []string {
	t.Helper()
	tokens := make([]string, 0, count)
	for i := 0; i < count; i++ {
		token := fmt.Sprintf("%s_%d", prefix, i)
		clk := sess.Clock()
		deadline := ptytest.Now(clk).Add(2 * time.Second)
		for {
			sess.Send("echo " + token + "\n")
			if screenContainsWithin(sess, token, 300*time.Millisecond) {
				break
			}
			if ptytest.Now(clk).After(deadline) {
				t.Fatalf("expected token %q in screen", token)
			}
			ptytest.Advance(clk, 100*time.Millisecond)
		}
		tokens = append(tokens, token)
		if i < count-1 {
			sess.SendCtrlL()
			sess.Send("n")
			ptytest.Advance(sess.Clock(), 300*time.Millisecond)
		}
	}
	return tokens
}

func cycleSendTokensWithActive(t *testing.T, sess *ptytest.PTYSession, count int, prefix string, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client) []string {
	t.Helper()
	tokens := make([]string, 0, count)
	current := waitForActiveSessionReady(t, clk, activeMu, active, viewsMu, views, "", 3*time.Second)
	for i := 0; i < count; i++ {
		token := fmt.Sprintf("%s_%d", prefix, i)
		deadline := ptytest.Now(clk).Add(2 * time.Second)
		for {
			sess.Send("echo " + token + "\n")
			if screenContainsWithin(sess, token, 300*time.Millisecond) {
				break
			}
			if ptytest.Now(clk).After(deadline) {
				t.Fatalf("expected token %q in screen", token)
			}
			ptytest.Advance(clk, 100*time.Millisecond)
		}
		tokens = append(tokens, token)
		if i < count-1 {
			current = advanceActiveTabWithRetry(t, sess, clk, activeMu, active, viewsMu, views, current, 2*time.Second)
		}
	}
	return tokens
}

func advanceActiveTabWithRetry(t *testing.T, sess *ptytest.PTYSession, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 150*time.Millisecond)
		if next, ok := tryWaitForActiveSessionReady(clk, activeMu, active, viewsMu, views, prev, 400*time.Millisecond); ok {
			return next
		}
	}
	t.Fatalf("timed out waiting for active session ready after %q", prev)
	return ""
}

func tryWaitForActiveSessionReady(clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) (string, bool) {
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		activeMu.Lock()
		current := *active
		activeMu.Unlock()
		if current != "" && current != prev {
			viewsMu.Lock()
			client := views[current]
			viewsMu.Unlock()
			if client != nil && client.Connected() {
				return current, true
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return "", false
}

func assertTokensVisibleAcrossTabs(t *testing.T, sess *ptytest.PTYSession, count int, tokens []string, label string) {
	t.Helper()
	found := make(map[string]bool, len(tokens))
	maxCycles := count * 4
	if maxCycles < count {
		maxCycles = count
	}
	for i := 0; i < maxCycles; i++ {
		for _, token := range tokens {
			if screenContainsWithin(sess, token, 500*time.Millisecond) {
				found[token] = true
			}
		}
		if len(found) == len(tokens) {
			return
		}
		sess.SendCtrlL()
		sess.Send("n")
		ptytest.Advance(sess.Clock(), 300*time.Millisecond)
	}
	missing := make([]string, 0)
	for _, token := range tokens {
		if !found[token] {
			missing = append(missing, token)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%s did not see tokens: %v", label, missing)
	}
}

func switchAttachToHost(t *testing.T, attachSess, host *ptytest.PTYSession, count int, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client) {
	t.Helper()
	current := waitForActiveSessionReady(t, clk, activeMu, active, viewsMu, views, "", 3*time.Second)
	for i := 0; i < count+1; i++ {
		token := fmt.Sprintf("ATTACH_HOSTB_%d", i)
		attachSess.Send("echo " + token + "\n")
		if screenContainsWithin(host, token, 300*time.Millisecond) {
			return
		}
		attachSess.SendCtrlL()
		attachSess.Send("n")
		ptytest.Advance(attachSess.Clock(), 150*time.Millisecond)
		current = waitForActiveSessionReady(t, clk, activeMu, active, viewsMu, views, current, 3*time.Second)
	}
	t.Fatalf("unable to switch attach to host tab")
}

func waitForTabLabels(t *testing.T, sess *ptytest.PTYSession, labels []string, timeout time.Duration) {
	t.Helper()
	lastToggle := time.Time{}
	sess.Eventually(timeout, 50*time.Millisecond, func(screen ptytest.Screen) error {
		snapshot := screen.String()
		for _, label := range labels {
			if !strings.Contains(snapshot, label) {
				if ptytest.Now(sess.Clock()).Sub(lastToggle) > 300*time.Millisecond {
					sess.SendCtrlL()
					sess.Send("b")
					lastToggle = ptytest.Now(sess.Clock())
				}
				return fmt.Errorf("missing label %q", label)
			}
		}
		return nil
	})
}
