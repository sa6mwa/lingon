//go:build integration

package integrationwebui_test

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/relay"
)

func TestWebUISessionManagement(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
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
	server := httptest.NewServer(backend.Handler())
	t.Cleanup(server.Close)
	access, err := store.CreateAccessToken("owner", time.Hour, time.Now())
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
	if err := chromedp.Run(ctx,
		chromedp.Click("#menu-toggle"),
		chromedp.Click("#rename-session-btn"),
		chromedp.SetValue("#rename-session-input", "Web build 🦊"),
		chromedp.Click("#rename-session-form button[type=submit]"),
		chromedp.Poll(`document.getElementById('tab-list').textContent.includes('Web build 🦊')`, nil),
	); err != nil {
		t.Fatal(err)
	}
	original, _ := store.GetSession("web-source")
	if original.Name != "Web build 🦊" {
		t.Fatalf("rename did not reach host: %+v", original)
	}
	var childID string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`if (document.getElementById("menu").classList.contains("hidden")) document.getElementById("menu-toggle").click()`, nil),
		chromedp.Click("#new-terminal-btn"),
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
}
