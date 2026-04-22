package attach_test

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachNeverBlankAfterMultiPTYLifecycle(t *testing.T) {
	restoreTabDelay := mvu.SetTabBarAutoHideDelay(24 * time.Hour)
	defer restoreTabDelay()

	var sessionsCount int64
	var wsClientCount int64

	h := newHarness(t, ptytest.WithRequestHook(func(r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			atomic.AddInt64(&sessionsCount, 1)
		case strings.HasSuffix(r.URL.Path, "/ws/client"):
			atomic.AddInt64(&wsClientCount, 1)
		}
	}))

	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Shell:     shell,
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	host.SendCtrlL()
	host.Send("c")
	host.SendCtrlL()
	host.Send("c")

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)
	labelMap := sessionLabelMap(t, h.Endpoint(), h.AccessToken())
	labels := labelsFromMap(labelMap)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     400 * time.Millisecond,
		RefreshInterval: 150 * time.Millisecond,
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
		OnViewClosed: func(sessionID string, _ bool, _ bool) {
			viewsMu.Lock()
			delete(views, sessionID)
			viewsMu.Unlock()
		},
	})

	waitForClientReady(t, h.Clock(), &viewsMu, views, "host-1", 3*time.Second)

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo ATTACH_BOOTSTRAP\n")
	if !screenContainsWithin(attachSess, "ATTACH_BOOTSTRAP", 3*time.Second) {
		t.Fatalf("expected attach bootstrap output before asserting tab labels")
	}
	attachSess.Eventually(3*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		cur := attachSess.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("expected active cursor below row 1 before asserting tab labels; got row %d col %d", cur.Row, cur.Col)
		}
		return nil
	})

	attachSess.SendCtrlL()
	attachSess.Send("b")
	waitForTabLabels(t, attachSess, labels, 5*time.Second)

	for i := 0; i < 3; i++ {
		token := fmt.Sprintf("ATTACH_READY_%d", i)
		attachSess.Send("echo " + token + "\n")
		if !screenContainsWithin(attachSess, token, 3*time.Second) {
			t.Fatalf("expected attach output %q", token)
		}
		if screenIsBlank(attachSess.Screen()) {
			t.Fatalf("screen went blank after token %q", token)
		}
		if i < 2 {
			attachSess.SendCtrlL()
			attachSess.Send("n")
			h.Advance(150 * time.Millisecond)
			currentActive = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)
		}
	}

	attachSess.SendCtrlL()
	attachSess.Send("p")
	h.Advance(700 * time.Millisecond)

	host.SendCtrlL()
	host.Send("n")
	host.SendBytes([]byte{0x04})

	removedID := waitForRemovedSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), labelMap, 5*time.Second)
	if removedID == "" {
		t.Fatalf("expected a session removal after host ctrl+d")
	}

	attachSess.SendCtrlL()
	attachSess.Send("b")
	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if label := labelMap[removedID]; label != "" {
			for _, field := range strings.Fields(row) {
				if field == label {
					return fmt.Errorf("expected removed tab %q to disappear, row=%q", label, row)
				}
			}
		}
		return nil
	})

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo ATTACH_ALIVE\n")
	if !screenContainsWithin(attachSess, "ATTACH_ALIVE", 3*time.Second) {
		t.Fatalf("expected attach output after removal")
	}
	if screenIsBlank(attachSess.Screen()) {
		t.Fatalf("screen went blank after removal")
	}

	startSessions := atomic.LoadInt64(&sessionsCount)
	startWS := atomic.LoadInt64(&wsClientCount)
	h.Advance(2 * time.Second)
	deltaSessions := atomic.LoadInt64(&sessionsCount) - startSessions
	deltaWS := atomic.LoadInt64(&wsClientCount) - startWS
	if deltaSessions > 3 || deltaWS > 3 {
		t.Fatalf("unexpected request churn after steady state: sessions=%d ws=%d", deltaSessions, deltaWS)
	}
}

func waitForActiveSessionReady(t *testing.T, clk clock.Clock, activeMu *sync.Mutex, active *string, viewsMu *sync.Mutex, views map[string]*attach.Client, prev string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	for ptytest.Now(clk).Before(deadline) {
		activeMu.Lock()
		current := *active
		activeMu.Unlock()
		if current != "" && (prev == "" || current != prev) {
			if clientReady(viewsMu, views, current) {
				return current
			}
		}
		if fallback := readySessionID(viewsMu, views, prev); fallback != "" {
			return fallback
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	if prev == "" {
		t.Fatalf("timed out waiting for active session ready")
	}
	t.Fatalf("timed out waiting for active session ready after %q", prev)
	return ""
}

func clientReady(viewsMu *sync.Mutex, views map[string]*attach.Client, id string) bool {
	viewsMu.Lock()
	client := views[id]
	viewsMu.Unlock()
	return client != nil && client.Connected()
}

func readySessionID(viewsMu *sync.Mutex, views map[string]*attach.Client, exclude string) string {
	viewsMu.Lock()
	ids := make([]string, 0, len(views))
	for id, client := range views {
		if id == exclude {
			continue
		}
		if client != nil && client.Connected() {
			ids = append(ids, id)
		}
	}
	viewsMu.Unlock()
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func screenIsBlank(screen ptytest.Screen) bool {
	for row := 0; row < screen.Rows; row++ {
		if strings.TrimSpace(screen.Row(row)) != "" {
			return false
		}
	}
	return true
}

func waitForRemovedSession(t *testing.T, clk clock.Clock, endpoint, token string, labelMap map[string]string, timeout time.Duration) string {
	t.Helper()
	deadline := ptytest.Now(clk).Add(timeout)
	known := make(map[string]struct{}, len(labelMap))
	for id := range labelMap {
		known[id] = struct{}{}
	}
	for ptytest.Now(clk).Before(deadline) {
		ids, err := fetchSessionIDs(endpoint, token)
		if err == nil {
			for id := range known {
				if !ids[id] {
					return id
				}
			}
		}
		ptytest.Advance(clk, 50*time.Millisecond)
	}
	return ""
}

func TestAttachSwitchesAfterActivePTYExit(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	secondID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)

	afterSecond, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions after second: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	thirdID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), afterSecond, 5*time.Second)

	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     1 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
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

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo ACTIVE_BEFORE_EXIT\n")
	if !screenContainsWithin(attachSess, "ACTIVE_BEFORE_EXIT", 3*time.Second) {
		t.Fatalf("expected attach output before exit")
	}

	host.SendBytes([]byte{0x04})
	waitForSessionRemovalByID(t, h.Clock(), h.Endpoint(), h.AccessToken(), thirdID, 5*time.Second)
	h.Advance(500 * time.Millisecond)

	attachSess.Eventually(5*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		if screenIsBlank(screen) {
			return fmt.Errorf("screen went blank after active exit")
		}
		return nil
	})
	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo ACTIVE_AFTER_EXIT\n")
	if !screenContainsWithin(attachSess, "ACTIVE_AFTER_EXIT", 3*time.Second) {
		t.Fatalf("expected attach output after exit")
	}
	_ = secondID
}

func TestAttachRapidTabSwitchKeepsIO(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	host.SendCtrlL()
	host.Send("c")
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       "host-1",
		Cols:            120,
		Rows:            30,
		InactiveTTL:     2 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
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

	currentActive := waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	for i := 0; i < 9; i++ {
		token := fmt.Sprintf("RAPID_%d", i)
		attachSess.Send("echo " + token + "\n")
		if !screenContainsWithin(attachSess, token, 3*time.Second) {
			t.Fatalf("expected output %q", token)
		}
		if screenIsBlank(attachSess.Screen()) {
			t.Fatalf("screen went blank during rapid switching")
		}
		attachSess.SendCtrlL()
		attachSess.Send("n")
		h.Advance(100 * time.Millisecond)
		currentActive = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, currentActive, 3*time.Second)
	}
}

func TestAttachActiveSessionExitActivatesNextImmediately(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1"})

	beforeIDs, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions before: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	secondID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), beforeIDs, 5*time.Second)

	afterSecond, err := fetchSessionIDs(h.Endpoint(), h.AccessToken())
	if err != nil {
		t.Fatalf("fetch sessions after second: %v", err)
	}
	host.SendCtrlL()
	host.Send("c")
	thirdID := waitForNewSessionIDFromSet(t, h.Clock(), h.Endpoint(), h.AccessToken(), afterSecond, 5*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 3, 5*time.Second)

	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	var activeMu sync.Mutex
	activeID := ""
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:       thirdID,
		Cols:            120,
		Rows:            30,
		InactiveTTL:     2 * time.Second,
		RefreshInterval: 150 * time.Millisecond,
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

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
	attachSess.Send("echo ACTIVE_SESSION_BEFORE\n")
	if !screenContainsWithin(attachSess, "ACTIVE_SESSION_BEFORE", 3*time.Second) {
		t.Fatalf("expected attach output before active exit")
	}

	host.SendBytes([]byte{0x04})
	waitUntil(t, h.Clock(), 3*time.Second, func() bool {
		activeMu.Lock()
		currentActive := activeID
		activeMu.Unlock()
		return currentActive != "" && currentActive != thirdID
	})

	waitForSessionRemovalByID(t, h.Clock(), h.Endpoint(), h.AccessToken(), thirdID, 10*time.Second)
	waitForSessionCount(t, h.Clock(), h.Endpoint(), h.AccessToken(), 2, 10*time.Second)

	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Send("echo ACTIVE_SESSION_AFTER\n")
	if !screenContainsWithin(attachSess, "ACTIVE_SESSION_AFTER", 3*time.Second) {
		t.Fatalf("expected attach output after active exit")
	}
	_ = secondID
}
