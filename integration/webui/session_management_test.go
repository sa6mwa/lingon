//go:build integration

package integrationwebui_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/relay"
)

func TestWebUISessionManagement(t *testing.T) {
	testWebUISessionManagement(t, false)
}

func TestWebUIManagementAuthRecovery(t *testing.T) {
	testWebUISessionManagement(t, true)
}

func testWebUISessionManagement(t *testing.T, expiredAccess bool) {
	t.Helper()
	dir := t.TempDir()
	cfg := dir // Keep generated Unix socket paths below the platform limit.
	binary := filepath.Join(dir, "lingon")
	build := exec.Command("go", "build", "-o", binary, "./cmd/lingon")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	backend := relay.NewHTTPServer(store, nil, nil, nil, hub)
	backend.DataDir = cfg
	server := httptest.NewTLSServer(backend.Handler())
	t.Cleanup(server.Close)
	access, err := store.CreateAccessToken("owner", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := store.CreateRefreshToken("owner", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := store.CreateAccessToken("owner", -time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	start := exec.Command(binary, "-x", "--config-dir", cfg, "--session", "web-source", "--shell", "/bin/sh", "--endpoint", server.URL, "--token", access.Token, "--insecure")
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("launch isolated headless terminal: %v %s", err, output)
	}
	t.Cleanup(func() {
		if records, err := headless.NewStore(cfg).Reconcile(); err == nil {
			for _, rec := range records {
				_ = headless.DetachSession(context.Background(), cfg, rec.SessionID)
			}
		}
	})
	allocator, stopAllocator := chromedp.NewExecAllocator(context.Background(), chromedpAllocatorOptions(filepath.Join(dir, "chrome"))...)
	defer stopAllocator()
	ctx, stopBrowser := chromedp.NewContext(allocator)
	defer stopBrowser()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable(), network.SetCookie("lingon_access", access.Token).WithURL(server.URL).WithPath("/"), chromedp.Navigate(server.URL+"/")); err != nil {
		t.Fatal(err)
	}
	waitForTerminalView(t, ctx, 15*time.Second)
	waitForTerminalReady(t, ctx, 15*time.Second)
	if err := chromedp.Run(ctx, network.SetCookie("lingon_refresh", refresh.Token).WithURL(server.URL).WithPath("/"),
		chromedp.Evaluate(`(async () => {
window.__managementCalls = [];
const originalFetch = window.fetch;
let cachedSessions = await (await originalFetch('sessions')).json();
let refreshAfterMutation = false;
window.fetch = async (...args) => {
  const fault = window.__managementFault;
  if (fault && (args[0] === 'sessions/manage' || args[0] === 'auth/refresh')) {
    const refresh = args[0] === 'auth/refresh';
    const status = refresh ? (fault === 'refresh-rejected' ? 401 : 200) :
      ({lost: 0, malformed: 200, forbidden: 403, server: 500}[fault] ?? 401);
    window.__managementCalls.push({path: args[0], body: args[1].body, status});
    if (status === 0) throw new TypeError('connection lost');
    return new Response(fault === 'malformed' ? '{' : JSON.stringify({error: refresh ? '' : 'rejected'}), {status});
  }
  // Keep unrelated periodic listing from refreshing the deliberately expired
  // cookie. Management and its resulting list refresh still use the real relay.
  if (args[0] === 'sessions' && !refreshAfterMutation && !window.__managementLiveSessions)
    return new Response(JSON.stringify(cachedSessions), {status: 200});
  const response = await originalFetch(...args);
  if (args[0] === 'sessions/manage' || args[0] === 'auth/refresh')
    window.__managementCalls.push({path: args[0], body: args[1].body, status: response.status,
      result: args[0] === 'sessions/manage' ? await response.clone().text() : ''});
  if (args[0] === 'sessions/manage' && response.ok) refreshAfterMutation = true;
  if (args[0] === 'sessions') {
    cachedSessions = await response.clone().json();
    refreshAfterMutation = false;
  }
  return response;
};
})()`, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatal(err)
	}
	prepareManagement := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__managementCalls = [];`, nil)); err != nil {
			t.Fatal(err)
		}
		if expiredAccess {
			if err := chromedp.Run(ctx, network.SetCookie("lingon_access", expired.Token).WithURL(server.URL).WithPath("/")); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertManagementRequests := func() {
		t.Helper()
		var calls []struct {
			Path   string `json:"path"`
			Body   string `json:"body"`
			Status int    `json:"status"`
			Result string `json:"result"`
		}
		if err := chromedp.Run(ctx, chromedp.Poll(`!document.getElementById('new-terminal-btn').disabled && window.__managementCalls.some(c => c.path === 'sessions/manage')`, nil),
			chromedp.Evaluate(`window.__managementCalls`, &calls)); err != nil {
			var debug string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({calls: window.__managementCalls, state: window.__bifrons.debug.state, status: document.getElementById('status-banner').textContent})`, &debug))
			t.Fatalf("management completion: %v %s", err, debug)
		}
		if expiredAccess {
			if len(calls) != 3 || calls[0].Path != "sessions/manage" || calls[0].Status != 401 ||
				calls[1].Path != "auth/refresh" || calls[1].Status != 200 || calls[2].Path != "sessions/manage" ||
				calls[2].Status != 200 || calls[0].Body != calls[2].Body {
				t.Fatalf("expected one refresh and a retry with identical request ID/payload: %+v", calls)
			}
		} else if len(calls) != 1 || calls[0].Path != "sessions/manage" || calls[0].Status != 200 {
			t.Fatalf("fresh credentials should execute once without refreshing: %+v", calls)
		}
		var result struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(calls[len(calls)-1].Result), &result); err != nil || result.Error != "" {
			t.Fatalf("management did not complete: %v %+v", err, calls)
		}
	}
	prepareManagement()
	if err := chromedp.Run(ctx,
		chromedp.Click("#menu-toggle"),
		chromedp.Click("#rename-session-btn"),
		chromedp.SetValue("#rename-session-input", "Web build 🦊"),
		chromedp.Click("#rename-session-form button[type=submit]"),
	); err != nil {
		t.Fatalf("renamed tab did not update: %v", err)
	}
	assertManagementRequests()
	t.Log("rename HTTP sequence verified")
	if err := chromedp.Run(ctx, chromedp.Poll(`document.getElementById('tab-list').textContent.includes('Web build 🦊')`, nil)); err != nil {
		var debug string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({calls: window.__managementCalls, state: window.__bifrons.debug.state, tabs: document.getElementById('tab-list').textContent, status: document.getElementById('status-banner').textContent})`, &debug))
		original, _ := store.GetSession("web-source")
		t.Fatalf("renamed tab: %v relay=%+v browser=%s", err, original, debug)
	}
	original, _ := store.GetSession("web-source")
	if original.Name != "Web build 🦊" {
		t.Fatalf("rename did not reach host: %+v", original)
	}
	t.Log("rename UI and relay metadata verified")
	var childID string
	prepareManagement()
	t.Log("create credentials prepared")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`if (document.getElementById("menu").classList.contains("hidden")) document.getElementById("menu-toggle").click()`, nil),
		chromedp.Click("#new-terminal-btn"),
	); err != nil {
		t.Fatal(err)
	}
	assertManagementRequests()
	t.Log("create HTTP sequence verified")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`window.__bifrons && window.__bifrons.debug && window.__bifrons.debug.state.activeSessionId.startsWith('terminal-')`, nil),
		chromedp.Evaluate(`window.__bifrons.debug.state.activeSessionId`, &childID),
	); err != nil {
		t.Fatal(err)
	}
	waitForTerminalReady(t, ctx, 15*time.Second)
	child, ok := store.GetSession(childID)
	if !ok || !child.Headless || child.Username != "owner" {
		t.Fatalf("new terminal metadata: %+v", child)
	}
	// The renamed source socket remains at its original identity and accepts reconnects.
	state, err := headless.NewStore(cfg).Load()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := headless.SocketPath(cfg, "web-source")
	if state.Sessions["web-source"].SocketPath != path || state.Sessions["web-source"].Name != "Web build 🦊" {
		t.Fatal("rename changed socket identity or was not retained by host")
	}
	if !expiredAccess {
		for _, fault := range []struct {
			name  string
			count int
		}{
			{"lost", 1}, {"malformed", 1}, {"forbidden", 1}, {"server", 1},
			{"refresh-rejected", 2}, {"repeated-401", 3},
		} {
			t.Run(fault.name, func(t *testing.T) {
				var calls []struct {
					Path   string `json:"path"`
					Body   string `json:"body"`
					Status int    `json:"status"`
				}
				var status string
				if err := chromedp.Run(ctx,
					chromedp.Evaluate("window.__managementFault = "+strconv.Quote(fault.name)+"; window.__managementCalls = [];", nil),
					chromedp.Evaluate(`if (document.getElementById('menu').classList.contains('hidden')) document.getElementById('menu-toggle').click()`, nil),
					chromedp.Click("#rename-session-btn"), chromedp.SetValue("#rename-session-input", "Must not execute"),
					chromedp.Click("#rename-session-form button[type=submit]"),
					chromedp.Poll(`!document.getElementById('new-terminal-btn').disabled && window.__managementCalls.length > 0`, nil),
					chromedp.Evaluate(`window.__managementCalls`, &calls), chromedp.Text("#status-banner", &status),
				); err != nil {
					t.Fatal(err)
				}
				if len(calls) != fault.count {
					t.Fatalf("unexpected retry/refresh: %+v", calls)
				}
				if fault.count == 3 && calls[0].Body != calls[2].Body {
					t.Fatal("401 retry changed the operation identity")
				}
				if (fault.name == "lost" || fault.name == "malformed") && !strings.Contains(status, "outcome unknown") {
					t.Fatalf("uncertain outcome not reported: %q", status)
				}
				if fault.name != "lost" && fault.name != "malformed" && status != "rejected" {
					t.Fatalf("new operation retained a previous outcome: %q", status)
				}
				if fault.name == "lost" || fault.name == "malformed" {
					// Drive the real reconnect path after the operation settles.
					// Connectivity updates must retain an uncertain management result.
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(`window.__managementFault = ''; window.__managementLiveSessions = true;
window.__managementOrigin = window.__bifrons.debug.state.activeSessionId;
window.__managementOriginLabel = document.querySelector('#tab-list .tab-button.active').textContent;
window.__bifrons.forceDisconnect(); document.getElementById('status-banner').textContent`, &status),
					); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(status, "outcome unknown") {
						t.Fatalf("reconnect callback erased uncertain outcome: %q", status)
					}
					if err := chromedp.Run(ctx,
						chromedp.Poll(`window.__bifrons.viewInfo().wsState === WebSocket.OPEN && !window.__bifrons.viewInfo().reconnectPending`, nil),
						// Discovery can select another online terminal while the
						// origin reconnects; return to the operation's own tab.
						chromedp.Evaluate(`Array.from(document.querySelectorAll('#tab-list .tab-button')).find(button => button.textContent === window.__managementOriginLabel).click()`, nil),
						chromedp.Poll(`window.__bifrons.debug.state.activeSessionId === window.__managementOrigin && window.__bifrons.viewInfo().wsState === WebSocket.OPEN && !window.__bifrons.viewInfo().reconnectPending`, nil),
						chromedp.Text("#status-banner", &status),
					); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(status, "outcome unknown") {
						t.Fatalf("recovered connection erased uncertain outcome: %q", status)
					}
				}
			})
		}
	}
}
