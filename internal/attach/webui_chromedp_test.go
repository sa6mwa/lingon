//go:build webui
// +build webui

package attach

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func newTestClock() clock.Clock {
	return clock.New()
}

func TestWebUIControlFlow(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostInput := &byteCollector{}
	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_test",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/cat"},
		OnInput: func(data []byte) {
			hostInput.Add(data)
		},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost("session_test")
	}, hostErr)

	in1, w1 := io.Pipe()
	defer w1.Close()
	out1 := &bytes.Buffer{}
	size1 := &sizeProvider{cols: 80, rows: 24}
	c1 := &Client{
		Endpoint:       endpoint,
		SessionID:      "session_test",
		AccessToken:    access.Token,
		RequestControl: true,
		ClientID:       "client1",
		Stdin:          in1,
		Stdout:         out1,
		Stderr:         io.Discard,
		TermSize:       size1.Size,
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	t.Cleanup(cancel1)
	c1Err := make(chan error, 1)
	go func() {
		c1Err <- c1.Run(ctx1)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		c1.mu.RLock()
		defer c1.mu.RUnlock()
		return c1.holderID == "client1"
	}, hostErr, c1Err)

	_, _ = w1.Write([]byte("HELLO\r\n"))

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	userDataDir := filepath.Join(home, "chromedp")
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		t.Fatalf("user data dir: %v", err)
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedpAllocatorOptions(userDataDir)...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)
	waitForTerminalReady(t, ctx, 15*time.Second, hostErr)
	waitUntilDebug(t, 10*time.Second, func() bool {
		text, err := xtermText(ctx)
		return err == nil && strings.Contains(text, "HELLO")
	}, func() string {
		text, err := xtermText(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("xterm text read failed: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("xterm text read failed: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("terminal missing rendered HELLO, text=%q debug=%s", text, debug)
		}
		return fmt.Sprintf("terminal missing rendered HELLO, text=%q", text)
	}, hostErr)
	waitUntilDebug(t, 5*time.Second, func() bool {
		banner, err := readStatusBanner(ctx)
		if err != nil {
			return false
		}
		return !banner.Visible && !strings.Contains(strings.ToLower(banner.Text), "control not permitted")
	}, func() string {
		banner, err := readStatusBanner(ctx)
		if err != nil {
			return fmt.Sprintf("status banner read failed: %v", err)
		}
		return fmt.Sprintf("unexpected status banner after attach: visible=%v text=%q", banner.Visible, banner.Text)
	}, hostErr)

	var cookiesAfterLogin []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}
		cookiesAfterLogin = cookies
		return nil
	})); err != nil {
		t.Fatalf("chromedp cookies: %v", err)
	}
	if !hasCookie(cookiesAfterLogin, "lingon_access") || !hasCookie(cookiesAfterLogin, "lingon_refresh") {
		t.Fatalf("expected auth cookies after login, got %+v", cookiesAfterLogin)
	}

	var viewState struct {
		LoginHidden   bool   `json:"loginHidden"`
		TerminalShown bool   `json:"terminalShown"`
		InitStep      string `json:"initStep"`
		LoginReason   string `json:"loginReason"`
		InitError     string `json:"initError"`
	}
	var sessionStatus int
	var hasTerm bool
	var authState string
	var storageStatus string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(loginURL),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`(() => {
  const login = document.getElementById("login-view");
  const term = document.getElementById("terminal-view");
  const lingon = window.__bifrons || {};
  return {
    loginHidden: login ? login.classList.contains("hidden") : false,
    terminalShown: term ? !term.classList.contains("hidden") : false,
    initStep: lingon.initStep || "",
    loginReason: lingon.lastLoginReason || "",
    initError: lingon.initError || "",
  };
	})()`, &viewState),
		chromedp.Evaluate(`(async () => {
  try {
    const resp = await fetch("sessions", { credentials: "include" });
    return resp.status;
  } catch {
    return 0;
  }
})()`, &sessionStatus, chromedp.EvalAsValue, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.Evaluate(`Boolean(window.__bifrons && window.__bifrons.term)`, &hasTerm),
		chromedp.Evaluate(`window.__bifrons ? window.__bifrons.authState || "" : ""`, &authState),
		chromedp.Evaluate(`(() => {
  try {
    localStorage.getItem("bifrons.fontScale");
    return "ok";
  } catch (err) {
    return err && err.name ? err.name : "error";
  }
})()`, &storageStatus),
	); err != nil {
		t.Fatalf("chromedp reload: %v", err)
	}
	if !viewState.LoginHidden || !viewState.TerminalShown {
		t.Fatalf("expected terminal view after reload, got loginHidden=%v terminalShown=%v initStep=%q loginReason=%q initError=%q sessionsStatus=%d termReady=%v authState=%q storage=%q", viewState.LoginHidden, viewState.TerminalShown, viewState.InitStep, viewState.LoginReason, viewState.InitError, sessionStatus, hasTerm, authState, storageStatus)
	}
	if viewState.LoginReason != "" {
		t.Fatalf("expected empty login reason after reload, got %q (initStep=%q authState=%q sessionsStatus=%d)", viewState.LoginReason, viewState.InitStep, authState, sessionStatus)
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, err := readViewInfo(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view info error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("view info error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
		}
		return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
	}, hostErr, c1Err)

	var pasted bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
  if (window.__bifrons && window.__bifrons.term) {
    window.__bifrons.term.paste("WEBINPUT\r\n");
    return true;
  }
  return false;
})()`, &pasted),
	); err != nil {
		t.Fatalf("chromedp paste: %v", err)
	}
	if !pasted {
		t.Fatalf("terminal not ready for paste")
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		return strings.Contains(hostInput.String(), "WEBINPUT")
	}, func() string {
		return "host missing WEBINPUT; last input: " + hostInput.String()
	}, hostErr, c1Err)
}

func TestWebUIAccountSeparation(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	userA, err := relay.CreateUser(users, "webuser-a", "pass-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := relay.CreateUser(users, "webuser-b", "pass-b", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	accessA, err := store.CreateAccessToken(userA.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken A: %v", err)
	}
	accessB, err := store.CreateAccessToken(userB.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken B: %v", err)
	}

	hostErr := make(chan error, 2)
	hostA := &host.Host{
		Endpoint:  endpoint,
		Token:     accessA.Token,
		SessionID: "web-session-a",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY_A\\r\\n'; sleep 3600"},
	}
	hostB := &host.Host{
		Endpoint:  endpoint,
		Token:     accessB.Token,
		SessionID: "web-session-b",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY_B\\r\\n'; sleep 3600"},
	}
	ctxHost, cancelHost := context.WithCancel(context.Background())
	t.Cleanup(cancelHost)
	go func() { hostErr <- hostA.Run(ctxHost) }()
	go func() { hostErr <- hostB.Run(ctxHost) }()

	waitUntil(t, clk, 5*time.Second, func() bool { return hub.HasHost("web-session-a") }, hostErr)
	waitUntil(t, clk, 5*time.Second, func() bool { return hub.HasHost("web-session-b") }, hostErr)

	loginURL := endpoint + "/"
	viewReadyTimeout := 30 * time.Second
	wsReadyTimeout := 20 * time.Second

	runLogin := func(user, pass, totpSecret, sessionID, readyToken, forbiddenToken, dataDir string) {
		ctx, cancel := newChromedpContext(t, dataDir)
		defer cancel()
		if err := chromedp.Run(ctx, network.Enable()); err != nil {
			t.Fatalf("chromedp network enable: %v", err)
		}
		code := totpCode(t, totpSecret)
		if err := ensureWebUILogin(ctx, loginURL, user, pass, code); err != nil {
			t.Fatalf("chromedp login %s: %v", user, err)
		}
		if err := ensureWebUILogin(ctx, loginURL, user, pass, code); err != nil {
			t.Fatalf("chromedp login retry %s: %v", user, err)
		}
		waitForTerminalView(t, ctx, viewReadyTimeout, hostErr)
		waitForTerminalReady(t, ctx, viewReadyTimeout, hostErr)

		waitUntilDebug(t, wsReadyTimeout, func() bool {
			ids, err := fetchSessionIDs(ctx)
			if err != nil {
				return false
			}
			return len(ids) == 1 && ids[0] == sessionID
		}, func() string {
			ids, _ := fetchSessionIDs(ctx)
			return fmt.Sprintf("sessions %s: %v", user, ids)
		}, hostErr)

		waitUntilDebug(t, wsReadyTimeout, func() bool {
			info, err := readViewInfo(ctx)
			return err == nil && info.WSState == 1
		}, func() string {
			info, err := readViewInfo(ctx)
			if err != nil {
				debug, _ := readDebugInfo(ctx)
				if debug != "" {
					return fmt.Sprintf("view info error: %v debug=%s", err, debug)
				}
				return fmt.Sprintf("view info error: %v", err)
			}
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
			}
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
		}, hostErr)

		waitUntilDebug(t, wsReadyTimeout, func() bool {
			info, err := readViewInfo(ctx)
			return err == nil && info.WSState == 1
		}, func() string {
			info, err := readViewInfo(ctx)
			if err != nil {
				debug, _ := readDebugInfo(ctx)
				if debug != "" {
					return fmt.Sprintf("view info error: %v debug=%s", err, debug)
				}
				return fmt.Sprintf("view info error: %v", err)
			}
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
			}
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
		}, hostErr)
	}

	runLogin(userA.User.Username, "pass-a", userA.TOTPSecret, "web-session-a", "READY_A", "READY_B", filepath.Join(home, "chromedp-a"))
	runLogin(userB.User.Username, "pass-b", userB.TOTPSecret, "web-session-b", "READY_B", "READY_A", filepath.Join(home, "chromedp-b"))

	shareA, err := store.CreateShareToken("web-session-a", relay.ShareScopeView, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken A: %v", err)
	}
	shareB, err := store.CreateShareToken("web-session-b", relay.ShareScopeView, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken B: %v", err)
	}

	runShare := func(token, readyToken, forbiddenToken, dataDir string) {
		ctx, cancel := newChromedpContext(t, dataDir)
		defer cancel()
		if err := ensureWebUIShare(ctx, loginURL, token); err != nil {
			t.Fatalf("chromedp share: %v", err)
		}
		waitForTerminalView(t, ctx, viewReadyTimeout, hostErr)
		waitForTerminalReady(t, ctx, viewReadyTimeout, hostErr)
		waitUntilDebug(t, wsReadyTimeout, func() bool {
			info, err := readViewInfo(ctx)
			return err == nil && info.WSState == 1
		}, func() string {
			info, err := readViewInfo(ctx)
			if err != nil {
				debug, _ := readDebugInfo(ctx)
				if debug != "" {
					return fmt.Sprintf("view info error: %v debug=%s", err, debug)
				}
				return fmt.Sprintf("view info error: %v", err)
			}
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
			}
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
		}, hostErr)
	}

	runShare(shareA.Token, "READY_A", "READY_B", filepath.Join(home, "chromedp-share-a"))
	runShare(shareB.Token, "READY_B", "READY_A", filepath.Join(home, "chromedp-share-b"))
}

func TestWebUIShareTokenCookieReloadAndLogout(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	user, err := relay.CreateUser(users, "share-user", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"
	loginURL := endpoint + "/"

	access, err := store.CreateAccessToken(user.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "share-session",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY_SHARE\\r\\n'; sleep 3600"},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost("share-session")
	}, hostErr)

	share, err := store.CreateShareToken("share-session", relay.ShareScopeView, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-share-cookie"))
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(loginURL+"?token="+url.QueryEscape(share.Token)),
	); err != nil {
		t.Fatalf("share attach login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)
	waitForTerminalReady(t, ctx, 15*time.Second, hostErr)

	var locationSearch string
	var locationHref string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.location.search`, &locationSearch),
		chromedp.Evaluate(`window.location.href`, &locationHref),
	); err != nil {
		t.Fatalf("read location: %v", err)
	}
	if locationSearch != "" {
		t.Fatalf("expected scrubbed URL search, got %q href=%q", locationSearch, locationHref)
	}
	if strings.Contains(locationHref, "token=") {
		t.Fatalf("expected token scrubbed from href, got %q", locationHref)
	}

	var shareSessionStatus int
	var sessionsStatus int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(async () => {
  const resp = await fetch("auth/share/session", { credentials: "include" });
  return resp.status;
})()`, &shareSessionStatus, chromedp.EvalAsValue, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.Evaluate(`(async () => {
  const resp = await fetch("sessions", { credentials: "include" });
  return resp.status;
})()`, &sessionsStatus, chromedp.EvalAsValue, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	); err != nil {
		t.Fatalf("share status checks: %v", err)
	}
	if shareSessionStatus != http.StatusOK {
		t.Fatalf("share session status = %d, want %d", shareSessionStatus, http.StatusOK)
	}
	if sessionsStatus != http.StatusUnauthorized {
		t.Fatalf("sessions status = %d, want %d", sessionsStatus, http.StatusUnauthorized)
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(loginURL)); err != nil {
		t.Fatalf("share reload navigate: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)
	waitForTerminalReady(t, ctx, 15*time.Second, hostErr)
	waitUntilDebug(t, 5*time.Second, func() bool {
		banner, err := readStatusBanner(ctx)
		if err != nil {
			return false
		}
		return !banner.Visible && !strings.Contains(strings.ToLower(banner.Text), "control not permitted")
	}, func() string {
		banner, err := readStatusBanner(ctx)
		if err != nil {
			return fmt.Sprintf("status banner read failed: %v", err)
		}
		return fmt.Sprintf("unexpected status banner after reload: visible=%v text=%q", banner.Visible, banner.Text)
	}, hostErr)

	if err := chromedp.Run(ctx,
		chromedp.Click("#menu-toggle", chromedp.ByQuery),
		chromedp.WaitVisible("#logout-btn", chromedp.ByQuery),
		chromedp.Click("#logout-btn", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("share logout click: %v", err)
	}
	waitForLoginView(t, ctx, 10*time.Second, hostErr)

	var afterLogoutShareStatus int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(async () => {
  const resp = await fetch("auth/share/session", { credentials: "include" });
  return resp.status;
})()`, &afterLogoutShareStatus, chromedp.EvalAsValue, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	); err != nil {
		t.Fatalf("share session after logout: %v", err)
	}
	if afterLogoutShareStatus != http.StatusUnauthorized {
		t.Fatalf("share session after logout status = %d, want %d", afterLogoutShareStatus, http.StatusUnauthorized)
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(loginURL)); err != nil {
		t.Fatalf("navigate after logout: %v", err)
	}
	waitForLoginView(t, ctx, 10*time.Second, hostErr)
}

func TestWebUIWebSocketBackoff(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_test",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/cat"},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost("session_test")
	}, hostErr)

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	userDataDir := filepath.Join(home, "chromedp")
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		t.Fatalf("user data dir: %v", err)
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedpAllocatorOptions(userDataDir)...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)

	waitUntil(t, clk, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, hostErr)
	before, err := readReconnectAt(ctx)
	if err != nil {
		t.Fatalf("read reconnect info: %v", err)
	}

	srv.CloseClientConnections()
	go func() {
		srv.Close()
	}()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__bifrons && window.__bifrons.forceDisconnect && window.__bifrons.forceDisconnect()`, nil)); err != nil {
		t.Fatalf("force disconnect: %v", err)
	}

	time.Sleep(6 * time.Second)

	after, err := readReconnectAt(ctx)
	if err != nil {
		t.Fatalf("read reconnect info: %v", err)
	}
	if after <= before {
		t.Fatalf("expected websocket reconnect attempts after server shutdown")
	}
}

func TestWebUIFullscreenSingleLayout(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"
	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostErr := make(chan error, 2)
	ctxHost, cancelHost := context.WithCancel(context.Background())
	t.Cleanup(cancelHost)
	hostA := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "fullscreen-a",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY_A\\r\\n'; sleep 3600"},
	}
	hostB := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "fullscreen-b",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY_B\\r\\n'; sleep 3600"},
	}
	go func() { hostErr <- hostA.Run(ctxHost) }()

	waitUntil(t, clk, 5*time.Second, func() bool { return hub.HasHost("fullscreen-a") }, hostErr)

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-fullscreen"))
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}
	code := totpCode(t, created.TOTPSecret)
	loginURL := endpoint + "/"
	if err := chromedp.Run(ctx,
		chromedp.Navigate(loginURL),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("chromedp navigate: %v", err)
	}
	waitUntilDebug(t, 20*time.Second, func() bool {
		var ready bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const login = document.getElementById("login-view");
  const form = document.getElementById("login-form");
  const terminal = document.getElementById("terminal-view");
  if (terminal && !terminal.classList.contains("hidden")) return true;
  return Boolean(login && form && !login.classList.contains("hidden"));
})()`, &ready))
		return ready
	}, func() string {
		return "login or terminal view not ready"
	}, hostErr)

	var terminalVisible bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const terminal = document.getElementById("terminal-view");
  return Boolean(terminal && !terminal.classList.contains("hidden"));
})()`, &terminalVisible)); err != nil {
		t.Fatalf("chromedp terminal visible check: %v", err)
	}
	if !terminalVisible {
		if err := chromedp.Run(ctx,
			chromedp.SendKeys("input[name='username']", created.User.Username, chromedp.ByQuery),
			chromedp.SendKeys("input[name='password']", "pass", chromedp.ByQuery),
			chromedp.SendKeys("input[name='totp']", code, chromedp.ByQuery),
			chromedp.Click("button[type='submit']", chromedp.ByQuery),
		); err != nil {
			t.Fatalf("chromedp login submit: %v", err)
		}
	}

	waitForTerminalReady(t, ctx, 60*time.Second, hostErr)

	waitUntilDebug(t, 10*time.Second, func() bool {
		ids, err := fetchSessionIDs(ctx)
		if err != nil {
			return false
		}
		return len(ids) >= 1
	}, func() string {
		return "expected at least 1 session"
	}, hostErr)

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const firstTab = document.querySelector(".tab-button");
  if (firstTab) {
    firstTab.click();
  }
  if (window.__bifrons && window.__bifrons.debug && window.__bifrons.debug.setFullscreenSingle) {
    window.__bifrons.debug.setFullscreenSingle(true);
  } else {
    const toggle = document.getElementById("fullscreen-toggle");
    if (toggle) {
      toggle.checked = true;
      toggle.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }
  if (window.__bifrons && window.__bifrons.debug && window.__bifrons.debug.setFullscreenActive) {
    window.__bifrons.debug.setFullscreenActive(true);
  }
  return true;
})()`, nil)); err != nil {
		t.Fatalf("chromedp fullscreen toggle: %v", err)
	}

	type layoutState struct {
		VisibleTiles int     `json:"visibleTiles"`
		LeftFits     bool    `json:"leftFits"`
		RightFits    bool    `json:"rightFits"`
		BottomFits   bool    `json:"bottomFits"`
		FillsScreen  bool    `json:"fillsScreen"`
		WidthRatio   float64 `json:"widthRatio"`
		HeightRatio  float64 `json:"heightRatio"`
	}
	measureLayout := func() (layoutState, error) {
		var state layoutState
		err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const tiles = Array.from(document.querySelectorAll(".terminal-tile"));
  const visible = tiles.filter(t => !t.classList.contains("hidden") && t.offsetParent !== null);
	  const active = document.querySelector(".terminal-tile.is-active");
	  const grid = document.getElementById("term-container");
	  let leftFits = true;
	  let rightFits = true;
	  let bottomFits = true;
	  let fillsScreen = false;
	  let widthRatio = 0;
	  let heightRatio = 0;
	  if (active && grid) {
	    const tileRect = active.getBoundingClientRect();
	    const gridRect = grid.getBoundingClientRect();
	    leftFits = tileRect.left >= gridRect.left - 1;
	    rightFits = tileRect.right <= gridRect.right + 1;
	    bottomFits = tileRect.bottom <= gridRect.bottom + 1;
	    widthRatio = gridRect.width > 0 ? tileRect.width / gridRect.width : 0;
	    heightRatio = gridRect.height > 0 ? tileRect.height / gridRect.height : 0;
	    fillsScreen = widthRatio >= 0.9 || heightRatio >= 0.9;
	  }
	  return { visibleTiles: visible.length, leftFits, rightFits, bottomFits, fillsScreen, widthRatio, heightRatio };
	})()`, &state))
		return state, err
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		state, err := measureLayout()
		if err != nil {
			return false
		}
		return state.VisibleTiles == 1 && state.LeftFits && state.RightFits && state.BottomFits && state.FillsScreen
	}, func() string {
		state, err := measureLayout()
		if err != nil {
			return fmt.Sprintf("fullscreen single layout eval failed: %v", err)
		}
		return fmt.Sprintf("fullscreen single layout not settled (visible=%d leftFits=%t rightFits=%t bottomFits=%t fillsScreen=%t w=%.2f h=%.2f)",
			state.VisibleTiles, state.LeftFits, state.RightFits, state.BottomFits, state.FillsScreen, state.WidthRatio, state.HeightRatio)
	}, hostErr)

	initial, err := measureLayout()
	if err != nil {
		t.Fatalf("chromedp fullscreen eval (initial): %v", err)
	}
	if initial.VisibleTiles != 1 {
		t.Fatalf("expected 1 visible tile in fullscreen single, got %d", initial.VisibleTiles)
	}
	if !initial.LeftFits || !initial.RightFits {
		t.Fatalf("expected fullscreen single tile to fit grid width without clipping")
	}
	if !initial.BottomFits {
		t.Fatalf("expected fullscreen single tile to fit grid height without clipping")
	}
	if !initial.FillsScreen {
		t.Fatalf("expected fullscreen single tile to fill most of the grid area (width=%.2f height=%.2f)",
			initial.WidthRatio, initial.HeightRatio)
	}

	go func() { hostErr <- hostB.Run(ctxHost) }()
	waitUntil(t, clk, 5*time.Second, func() bool { return hub.HasHost("fullscreen-b") }, hostErr)
	waitUntilDebug(t, 10*time.Second, func() bool {
		ids, err := fetchSessionIDs(ctx)
		if err != nil {
			return false
		}
		return len(ids) >= 2
	}, func() string {
		return "expected at least 2 sessions after adding host"
	}, hostErr)
	waitUntilDebug(t, 10*time.Second, func() bool {
		state, err := measureLayout()
		if err != nil {
			return false
		}
		return state.VisibleTiles == 1 && state.LeftFits && state.RightFits && state.BottomFits && state.FillsScreen
	}, func() string {
		state, err := measureLayout()
		if err != nil {
			return fmt.Sprintf("fullscreen single layout eval failed after adding host: %v", err)
		}
		return fmt.Sprintf("fullscreen single layout not settled after adding host (visible=%d leftFits=%t rightFits=%t bottomFits=%t fillsScreen=%t w=%.2f h=%.2f)",
			state.VisibleTiles, state.LeftFits, state.RightFits, state.BottomFits, state.FillsScreen, state.WidthRatio, state.HeightRatio)
	}, hostErr)

	after, err := measureLayout()
	if err != nil {
		t.Fatalf("chromedp fullscreen eval (after): %v", err)
	}
	if after.VisibleTiles != 1 {
		t.Fatalf("expected 1 visible tile after adding host, got %d", after.VisibleTiles)
	}
	if !after.LeftFits || !after.RightFits || !after.BottomFits {
		t.Fatalf("expected fullscreen single tile to remain unclipped after adding host")
	}
	if after.WidthRatio < initial.WidthRatio*0.95 || after.HeightRatio < initial.HeightRatio*0.95 {
		t.Fatalf("expected fullscreen single layout to stay consistent after adding host (before w=%.2f h=%.2f after w=%.2f h=%.2f)",
			initial.WidthRatio, initial.HeightRatio, after.WidthRatio, after.HeightRatio)
	}

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
  if (window.__bifrons && window.__bifrons.debug && window.__bifrons.debug.setFullscreenSingle) {
    window.__bifrons.debug.setFullscreenSingle(false);
  } else {
    const toggle = document.getElementById("fullscreen-toggle");
    if (toggle) {
      toggle.checked = false;
      toggle.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }
  if (window.__bifrons && window.__bifrons.debug && window.__bifrons.debug.setFullscreenActive) {
    window.__bifrons.debug.setFullscreenActive(false);
  }
  return true;
})()`, nil),
		chromedp.EmulateViewport(1100, 430),
	); err != nil {
		t.Fatalf("chromedp non-fullscreen single resize prep: %v", err)
	}

	type nonFullscreenState struct {
		VisibleTiles  int  `json:"visibleTiles"`
		ActiveVisible bool `json:"activeVisible"`
	}
	measureNonFullscreen := func() (nonFullscreenState, error) {
		var state nonFullscreenState
		err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const tiles = Array.from(document.querySelectorAll(".terminal-tile"));
  const visible = tiles.filter(t => !t.classList.contains("hidden") && t.offsetParent !== null);
  const active = document.querySelector(".terminal-tile.is-active");
  const activeVisible = Boolean(active && !active.classList.contains("hidden") && active.offsetParent !== null);
  return { visibleTiles: visible.length, activeVisible };
})()`, &state))
		return state, err
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		state, err := measureNonFullscreen()
		if err != nil {
			return false
		}
		return state.VisibleTiles == 1 && state.ActiveVisible
	}, func() string {
		state, err := measureNonFullscreen()
		if err != nil {
			return fmt.Sprintf("non-fullscreen single layout eval failed: %v", err)
		}
		return fmt.Sprintf("expected one visible active tile after resize, got visible=%d activeVisible=%t",
			state.VisibleTiles, state.ActiveVisible)
	}, hostErr)

	nonFullscreen, err := measureNonFullscreen()
	if err != nil {
		t.Fatalf("chromedp non-fullscreen single layout eval: %v", err)
	}
	if nonFullscreen.VisibleTiles != 1 || !nonFullscreen.ActiveVisible {
		t.Fatalf("expected one visible active tile after non-fullscreen shrink, got visible=%d activeVisible=%t",
			nonFullscreen.VisibleTiles, nonFullscreen.ActiveVisible)
	}
}

func TestWebUITabOverflowAutoScrollAndFades(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"
	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	sessionIDs := []string{
		"tab-overflow-01",
		"tab-overflow-02",
		"tab-overflow-03",
		"tab-overflow-04",
		"tab-overflow-05",
	}

	hostErr := make(chan error, len(sessionIDs))
	ctxHost, cancelHost := context.WithCancel(context.Background())
	t.Cleanup(cancelHost)
	for _, id := range sessionIDs {
		sessionHost := &host.Host{
			Endpoint:  endpoint,
			Token:     access.Token,
			SessionID: id,
			Cols:      80,
			Rows:      24,
			Command:   []string{"/bin/sh", "-c", "printf 'READY\\r\\n'; sleep 3600"},
		}
		go func(h *host.Host) { hostErr <- h.Run(ctxHost) }(sessionHost)
	}

	for _, id := range sessionIDs {
		sessionID := id
		waitUntil(t, clk, 5*time.Second, func() bool { return hub.HasHost(sessionID) }, hostErr)
	}

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-tab-overflow"))
	defer cancel()
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.EmulateViewport(640, 460),
	); err != nil {
		t.Fatalf("chromedp setup: %v", err)
	}
	code := totpCode(t, created.TOTPSecret)
	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)

	type tabState struct {
		ButtonCount    int     `json:"buttonCount"`
		Overflow       bool    `json:"overflow"`
		LeftFade       bool    `json:"leftFade"`
		RightFade      bool    `json:"rightFade"`
		ActiveVisible  bool    `json:"activeVisible"`
		ScrollLeft     float64 `json:"scrollLeft"`
		TabBarHidden   bool    `json:"tabBarHidden"`
		HorizontalFits bool    `json:"horizontalFits"`
	}

	readTabState := func() (tabState, error) {
		var state tabState
		err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const tabBar = document.getElementById("tab-bar");
  const tabList = document.getElementById("tab-list");
  const buttons = tabList ? Array.from(tabList.querySelectorAll(".tab-button")) : [];
  const active = tabList ? tabList.querySelector(".tab-button.active") : null;
  const listRect = tabList ? tabList.getBoundingClientRect() : null;
  let activeVisible = false;
  if (active && listRect) {
    const rect = active.getBoundingClientRect();
    activeVisible = rect.left >= listRect.left - 1 && rect.right <= listRect.right + 1;
  }
  return {
    buttonCount: buttons.length,
    overflow: Boolean(tabList && tabList.scrollWidth > tabList.clientWidth + 1),
    leftFade: Boolean(tabBar && tabBar.classList.contains("has-overflow-left")),
    rightFade: Boolean(tabBar && tabBar.classList.contains("has-overflow-right")),
    activeVisible,
    scrollLeft: tabList ? tabList.scrollLeft : 0,
    tabBarHidden: Boolean(tabBar && tabBar.classList.contains("hidden")),
    horizontalFits: Boolean(tabList && tabList.clientHeight === tabList.offsetHeight),
  };
})()`, &state))
		return state, err
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		state, err := readTabState()
		if err != nil {
			return false
		}
		return !state.TabBarHidden && state.ButtonCount == len(sessionIDs) && state.Overflow && state.ActiveVisible && state.RightFade
	}, func() string {
		state, err := readTabState()
		if err != nil {
			return fmt.Sprintf("tab state eval failed: %v", err)
		}
		return fmt.Sprintf("tab overflow not ready (hidden=%t count=%d overflow=%t left=%t right=%t visible=%t scrollLeft=%.1f)",
			state.TabBarHidden, state.ButtonCount, state.Overflow, state.LeftFade, state.RightFade, state.ActiveVisible, state.ScrollLeft)
	}, hostErr)

	initial, err := readTabState()
	if err != nil {
		t.Fatalf("read initial tab state: %v", err)
	}
	if initial.ButtonCount != len(sessionIDs) {
		t.Fatalf("expected %d tab buttons, got %d", len(sessionIDs), initial.ButtonCount)
	}
	if !initial.Overflow {
		t.Fatalf("expected tab list overflow for narrow viewport")
	}
	if !initial.ActiveVisible {
		t.Fatalf("expected active tab to be auto-visible initially")
	}
	if !initial.RightFade {
		t.Fatalf("expected right fade while tabs are hidden to the right")
	}
	if !initial.HorizontalFits {
		t.Fatalf("expected no visible horizontal scrollbar in tab list")
	}

	var clicked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const tabList = document.getElementById("tab-list");
  if (!tabList) return false;
  const buttons = Array.from(tabList.querySelectorAll(".tab-button"));
  const last = buttons[buttons.length - 1];
  if (!last) return false;
  last.click();
  return true;
})()`, &clicked)); err != nil {
		t.Fatalf("click last tab: %v", err)
	}
	if !clicked {
		t.Fatalf("expected last tab click to succeed")
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		state, err := readTabState()
		if err != nil {
			return false
		}
		return state.ActiveVisible && state.ScrollLeft > initial.ScrollLeft+10 && state.LeftFade
	}, func() string {
		state, err := readTabState()
		if err != nil {
			return fmt.Sprintf("tab state eval failed after click: %v", err)
		}
		return fmt.Sprintf("expected active tab auto-scroll to last (left=%t right=%t visible=%t scrollLeft=%.1f initial=%.1f)",
			state.LeftFade, state.RightFade, state.ActiveVisible, state.ScrollLeft, initial.ScrollLeft)
	}, hostErr)
}

func TestWebUIHostBurstRepro(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostInput := &byteCollector{}
	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)

	h := &host.Host{
		Endpoint:         endpoint,
		Token:            access.Token,
		SessionID:        "session_burst",
		Cols:             80,
		Rows:             24,
		Command:          []string{"/bin/sh"},
		MaxReplayScreens: 2,
		OnInput: func(data []byte) {
			hostInput.Add(data)
		},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost("session_burst")
	}, hostErr)

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	userDataDir := filepath.Join(home, "chromedp")
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		t.Fatalf("user data dir: %v", err)
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedpAllocatorOptions(userDataDir)...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)
	waitForTerminalReady(t, ctx, 15*time.Second, hostErr)

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, err := readViewInfo(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view info error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("view info error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
		}
		return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
	}, hostErr)

	var inputBurst strings.Builder
	inputBurst.WriteString("cat >/dev/null <<'EOF'\r")
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&inputBurst, "WEBLINE%04d\r", i)
	}
	inputBurst.WriteString("WEB_INPUT_DONE\rEOF\r")

	var pasted bool
	inputJS := fmt.Sprintf(`(() => {
  if (window.__bifrons && window.__bifrons.term) {
    window.__bifrons.term.paste(%s);
    return true;
  }
  return false;
})()`, strconv.Quote(inputBurst.String()))
	if err := chromedp.Run(ctx, chromedp.Evaluate(inputJS, &pasted)); err != nil {
		t.Fatalf("chromedp input paste: %v", err)
	}
	if !pasted {
		t.Fatalf("terminal not ready for input paste")
	}

	waitUntilDebug(t, 30*time.Second, func() bool {
		input := hostInput.String()
		return strings.Contains(input, "WEBLINE") && len(input) > 1000
	}, func() string {
		return "host missing input burst; last input: " + hostInput.String()
	}, hostErr)

	outputCmd := "i=1; while [ $i -le 200 ]; do printf 'HOSTLINE%04d\\r\\n' \"$i\"; i=$((i+1)); done; echo HOST_BURST_DONE\r"
	outputJS := fmt.Sprintf(`(() => {
  if (window.__bifrons && window.__bifrons.term) {
    window.__bifrons.term.paste(%s);
    return true;
  }
  return false;
})()`, strconv.Quote(outputCmd))
	if err := chromedp.Run(ctx, chromedp.Evaluate(outputJS, &pasted)); err != nil {
		t.Fatalf("chromedp output paste: %v", err)
	}
	if !pasted {
		t.Fatalf("terminal not ready for output paste")
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, err := readViewInfo(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view info error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("view info error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
		}
		return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
	}, hostErr)

	finalCmds := "echo CMD1\recho CMD2\recho CMD3\recho CMD4\recho CMD5\recho FINAL_OK\r"
	finalJS := fmt.Sprintf(`(() => {
  if (window.__bifrons && window.__bifrons.term) {
    window.__bifrons.term.paste(%s);
    return true;
  }
  return false;
})()`, strconv.Quote(finalCmds))
	if err := chromedp.Run(ctx, chromedp.Evaluate(finalJS, &pasted)); err != nil {
		t.Fatalf("chromedp final paste: %v", err)
	}
	if !pasted {
		t.Fatalf("terminal not ready for final paste")
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, err := readViewInfo(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("view info error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("view info error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("view ws not open, state=%.0f attempt=%d debug=%s", info.WSState, info.ReconnectAttempt, debug)
		}
		return fmt.Sprintf("view ws not open, state=%.0f attempt=%d", info.WSState, info.ReconnectAttempt)
	}, hostErr)
}

func xtermText(ctx context.Context) (string, error) {
	var out string
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const term = window.__bifrons && window.__bifrons.term ? window.__bifrons.term : null;
  if (!term || !term.buffer || !term.buffer.active) return "";
  const buf = term.buffer.active;
  const lines = [];
  for (let i = 0; i < buf.length; i += 1) {
    const line = buf.getLine(i);
    if (!line) {
      lines.push("");
      continue;
    }
    lines.push(line.translateToString(true));
  }
  return lines.join("\n");
})()`, &out))
	return out, err
}

func waitForTerminalReady(t *testing.T, ctx context.Context, timeout time.Duration, errs ...<-chan error) {
	t.Helper()
	waitUntilDebug(t, timeout, func() bool {
		var ready bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`Boolean(window.__bifrons && window.__bifrons.term && window.__bifrons.term.buffer && window.__bifrons.term.buffer.active)`, &ready))
		return ready
	}, func() string {
		state, err := readUIState(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("terminal not ready: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("terminal not ready: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("terminal not ready: login=%v initStep=%q initError=%q hasTerm=%v authState=%q debug=%s",
				state.LoginVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState, debug)
		}
		return fmt.Sprintf("terminal not ready: login=%v initStep=%q initError=%q hasTerm=%v authState=%q",
			state.LoginVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState)
	}, errs...)
}

type uiState struct {
	TerminalVisible bool   `json:"terminalVisible"`
	LoginVisible    bool   `json:"loginVisible"`
	InitStep        string `json:"initStep"`
	InitError       string `json:"initError"`
	HasTerminal     bool   `json:"hasTerminal"`
	AuthState       string `json:"authState"`
}

type statusBannerState struct {
	Visible bool   `json:"visible"`
	Text    string `json:"text"`
	Level   string `json:"level"`
}

func readUIState(ctx context.Context) (uiState, error) {
	var state uiState
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const login = document.getElementById("login-view");
  const terminal = document.getElementById("terminal-view");
  const lingon = window.__bifrons || {};
  return {
    terminalVisible: Boolean(terminal && !terminal.classList.contains("hidden")),
    loginVisible: Boolean(login && !login.classList.contains("hidden")),
    initStep: lingon.initStep || "",
    initError: lingon.initError || "",
    hasTerminal: Boolean(lingon.term),
    authState: lingon.authState || "",
  };
})()`, &state))
	return state, err
}

func readStatusBanner(ctx context.Context) (statusBannerState, error) {
	var state statusBannerState
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const banner = document.getElementById("status-banner");
  if (!banner) {
    return { visible: false, text: "", level: "" };
  }
  return {
    visible: !banner.classList.contains("hidden"),
    text: (banner.textContent || "").trim(),
    level: banner.dataset && banner.dataset.level ? String(banner.dataset.level) : "",
  };
})()`, &state))
	return state, err
}

func waitForTerminalView(t *testing.T, ctx context.Context, timeout time.Duration, errs ...<-chan error) {
	t.Helper()
	waitUntilDebug(t, timeout, func() bool {
		state, err := readUIState(ctx)
		return err == nil && state.TerminalVisible
	}, func() string {
		state, err := readUIState(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("ui state error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("ui state error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("terminal not visible: login=%v initStep=%q initError=%q hasTerm=%v authState=%q debug=%s",
				state.LoginVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState, debug)
		}
		return fmt.Sprintf("terminal not visible: login=%v initStep=%q initError=%q hasTerm=%v authState=%q",
			state.LoginVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState)
	}, errs...)
}

func waitForLoginView(t *testing.T, ctx context.Context, timeout time.Duration, errs ...<-chan error) {
	t.Helper()
	waitUntilDebug(t, timeout, func() bool {
		state, err := readUIState(ctx)
		return err == nil && state.LoginVisible
	}, func() string {
		state, err := readUIState(ctx)
		if err != nil {
			debug, _ := readDebugInfo(ctx)
			if debug != "" {
				return fmt.Sprintf("ui state error: %v debug=%s", err, debug)
			}
			return fmt.Sprintf("ui state error: %v", err)
		}
		debug, _ := readDebugInfo(ctx)
		if debug != "" {
			return fmt.Sprintf("login not visible: terminal=%v initStep=%q initError=%q hasTerm=%v authState=%q debug=%s",
				state.TerminalVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState, debug)
		}
		return fmt.Sprintf("login not visible: terminal=%v initStep=%q initError=%q hasTerm=%v authState=%q",
			state.TerminalVisible, state.InitStep, state.InitError, state.HasTerminal, state.AuthState)
	}, errs...)
}

func ensureWebUILogin(ctx context.Context, loginURL, username, password, totp string) error {
	access, refresh, err := loginViaAPI(loginURL, username, password, totp)
	if err != nil {
		return err
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := network.SetCookie("lingon_access", access).
				WithURL(loginURL).
				WithHTTPOnly(true).
				WithSecure(true).
				Do(ctx); err != nil {
				return err
			}
			if err := network.SetCookie("lingon_refresh", refresh).
				WithURL(loginURL).
				WithHTTPOnly(true).
				WithSecure(true).
				Do(ctx); err != nil {
				return err
			}
			return nil
		}),
	); err != nil {
		return err
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, err := page.Navigate(loginURL).Do(ctx)
			return err
		}),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		return err
	}
	return nil
}

func ensureWebUIShare(ctx context.Context, loginURL, token string) error {
	shareSessionID, err := shareViaAPI(loginURL, token)
	if err != nil {
		return err
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := network.SetCookie("bifrons_share_session", shareSessionID).
				WithURL(loginURL).
				WithHTTPOnly(true).
				WithSecure(true).
				Do(ctx); err != nil {
				return err
			}
			return nil
		}),
	); err != nil {
		return err
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, err := page.Navigate(loginURL).Do(ctx)
			return err
		}),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		return err
	}
	return nil
}

func loginViaAPI(loginURL, username, password, totp string) (string, string, error) {
	payload := map[string]string{
		"username":    username,
		"password":    password,
		"totp":        totp,
		"client_type": "web",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	authURL := strings.TrimRight(loginURL, "/") + "/auth/login"
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}
	resp, err := client.Post(authURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("auth login failed: %s", strings.TrimSpace(string(msg)))
	}
	var access, refresh string
	for _, cookie := range resp.Cookies() {
		switch cookie.Name {
		case "lingon_access":
			access = cookie.Value
		case "lingon_refresh":
			refresh = cookie.Value
		}
	}
	if access == "" || refresh == "" {
		return "", "", fmt.Errorf("auth cookies not set")
	}
	return access, refresh, nil
}

func shareViaAPI(loginURL, token string) (string, error) {
	payload := map[string]string{"token": token}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	shareURL := strings.TrimRight(loginURL, "/") + "/auth/share"
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		defer transport.CloseIdleConnections()
	}
	resp, err := client.Post(shareURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("share auth failed: %s", strings.TrimSpace(string(msg)))
	}
	var shareSession string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "bifrons_share_session" {
			shareSession = cookie.Value
			break
		}
	}
	if shareSession == "" {
		return "", fmt.Errorf("share session cookie not set")
	}
	return shareSession, nil
}

func newChromedpContext(t *testing.T, userDataDir string) (context.Context, context.CancelFunc) {
	t.Helper()
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		t.Fatalf("user data dir: %v", err)
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedpAllocatorOptions(userDataDir)...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	ctx, timeoutCancel := context.WithTimeout(ctx, 90*time.Second)
	return ctx, func() {
		timeoutCancel()
		cancel()
		allocCancel()
	}
}

func chromedpAllocatorOptions(userDataDir string) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.UserDataDir(userDataDir),
	)
	if execPath := chromeExecPath(); execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}
	return opts
}

func chromeExecPath() string {
	if explicit := strings.TrimSpace(os.Getenv("LINGON_CHROME_PATH")); explicit != "" {
		return explicit
	}
	candidates := []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/snap/bin/chromium",
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}
	return code
}

func fetchSessionIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`(async () => {
  try {
    const resp = await fetch("sessions", { credentials: "include" });
    if (!resp.ok) return [];
    const sessions = await resp.json();
    return sessions.map(s => s.id);
  } catch {
    return [];
  }
})()`, &ids, chromedp.EvalAsValue, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	)
	return ids, err
}

type byteCollector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *byteCollector) Add(data []byte) {
	b.mu.Lock()
	b.buf.Write(data)
	b.mu.Unlock()
}

func (b *byteCollector) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type sizeProvider struct {
	cols int
	rows int
}

func (s *sizeProvider) Size() (int, int) {
	return s.cols, s.rows
}

func waitUntil(t *testing.T, _ clock.Clock, timeout time.Duration, fn func() bool, errs ...<-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ch := range errs {
			select {
			case err := <-ch:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			default:
			}
		}
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout")
}

func waitUntilDebug(t *testing.T, timeout time.Duration, fn func() bool, debug func() string, errs ...<-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ch := range errs {
			select {
			case err := <-ch:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			default:
			}
		}
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if debug != nil {
		t.Fatalf("condition not met before timeout: %s", debug())
	}
	t.Fatalf("condition not met before timeout")
}

type viewInfo struct {
	ReconnectAttempt int     `json:"reconnectAttempt"`
	ReconnectPending bool    `json:"reconnectPending"`
	WSState          float64 `json:"wsState"`
}

func readViewInfo(ctx context.Context) (viewInfo, error) {
	var info *viewInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  if (!window.__bifrons || !window.__bifrons.viewInfo) {
    return null;
  }
  return window.__bifrons.viewInfo();
})()`, &info))
	if err != nil {
		return viewInfo{}, err
	}
	if info == nil {
		return viewInfo{}, fmt.Errorf("view info unavailable")
	}
	return *info, nil
}

func readActiveSessionID(ctx context.Context) (string, error) {
	var out string
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  if (!window.__bifrons || !window.__bifrons.debug || !window.__bifrons.debug.state) {
    return "";
  }
  return window.__bifrons.debug.state.activeSessionId || "";
})()`, &out))
	return out, err
}

func readDebugInfo(ctx context.Context) (string, error) {
	var out string
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  if (!window.__bifrons || !window.__bifrons.debugInfo) {
    return "";
  }
  try {
    return JSON.stringify(window.__bifrons.debugInfo());
  } catch (err) {
    return "";
  }
})()`, &out))
	return out, err
}

func readReconnectAt(ctx context.Context) (float64, error) {
	var out float64
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  if (!window.__bifrons || !window.__bifrons.debug) {
    return 0;
  }
  return window.__bifrons.debug.lastReconnectAt || 0;
	})()`, &out))
	return out, err
}

func TestWebUIResizeDoesNotBypassReconnectBackoff(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_test",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/cat"},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost("session_test")
	}, hostErr)

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-backoff-resize"))
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostErr)

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, err := readViewInfo(ctx)
		if err != nil {
			return fmt.Sprintf("view info error: %v", err)
		}
		return fmt.Sprintf("view ws not open, state=%.0f attempt=%d pending=%t",
			info.WSState, info.ReconnectAttempt, info.ReconnectPending)
	}, hostErr)

	// Simulate last host disappearing; client should reconnect with backoff.
	hostCancel()
	waitUntil(t, clk, 5*time.Second, func() bool {
		return !hub.HasHost("session_test")
	})

	waitUntilDebug(t, 20*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		if err == nil {
			return info.ReconnectAttempt >= 1 && info.ReconnectPending
		}
		active, activeErr := readActiveSessionID(ctx)
		return activeErr == nil && active == ""
	}, func() string {
		info, err := readViewInfo(ctx)
		if err == nil {
			debug, _ := readDebugInfo(ctx)
			return fmt.Sprintf("expected reconnect pending or stale session removal, got attempt=%d pending=%t ws=%.0f debug=%s",
				info.ReconnectAttempt, info.ReconnectPending, info.WSState, debug)
		}
		active, _ := readActiveSessionID(ctx)
		debug, _ := readDebugInfo(ctx)
		return fmt.Sprintf("expected reconnect pending or stale session removal, viewErr=%v active=%q debug=%s", err, active, debug)
	})

	before, err := readViewInfo(ctx)
	if err != nil {
		active, activeErr := readActiveSessionID(ctx)
		if activeErr == nil && active == "" {
			return
		}
		t.Fatalf("read view info before resize: %v (active=%q activeErr=%v)", err, active, activeErr)
	}

	// Repeated viewport changes should not bypass the scheduled reconnect timer.
	for i := 0; i < 12; i += 1 {
		width := 1100
		if i%2 == 1 {
			width = 1030
		}
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(int64(width), 640)); err != nil {
			t.Fatalf("chromedp emulate viewport #%d: %v", i, err)
		}
		time.Sleep(40 * time.Millisecond)
	}

	after, err := readViewInfo(ctx)
	if err != nil {
		t.Fatalf("read view info after resize: %v", err)
	}
	if after.ReconnectAttempt != before.ReconnectAttempt {
		debug, _ := readDebugInfo(ctx)
		t.Fatalf("expected reconnect attempt to stay stable during resize while timer pending (before=%d after=%d pending=%t debug=%s)",
			before.ReconnectAttempt, after.ReconnectAttempt, after.ReconnectPending, debug)
	}
	if !after.ReconnectPending {
		debug, _ := readDebugInfo(ctx)
		t.Fatalf("expected reconnect timer to remain pending after resize burst (debug=%s)", debug)
	}
}

func TestWebUISwitchesToNewActiveSessionAfterNoHost(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	hostAID := "session-old"
	hostBID := "session-new"

	ctxHostA, cancelHostA := context.WithCancel(context.Background())
	t.Cleanup(cancelHostA)
	hostAErr := make(chan error, 1)
	hostA := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: hostAID,
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'OLD\\r\\n'; sleep 3600"},
	}
	go func() { hostAErr <- hostA.Run(ctxHostA) }()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost(hostAID)
	}, hostAErr)

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-switch-active"))
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second, hostAErr)

	waitUntilDebug(t, 10*time.Second, func() bool {
		info, err := readViewInfo(ctx)
		if err != nil || info.WSState != 1 {
			return false
		}
		active, err := readActiveSessionID(ctx)
		return err == nil && active == hostAID
	}, func() string {
		info, _ := readViewInfo(ctx)
		active, _ := readActiveSessionID(ctx)
		debug, _ := readDebugInfo(ctx)
		return fmt.Sprintf("expected active old session before switch (active=%q ws=%.0f attempt=%d pending=%t debug=%s)",
			active, info.WSState, info.ReconnectAttempt, info.ReconnectPending, debug)
	}, hostAErr)

	// Drop the old host so the web client enters no-host reconnect mode.
	cancelHostA()
	waitUntil(t, clk, 5*time.Second, func() bool {
		return !hub.HasHost(hostAID)
	})

	ctxHostB, cancelHostB := context.WithCancel(context.Background())
	t.Cleanup(cancelHostB)
	hostBErr := make(chan error, 1)
	hostB := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: hostBID,
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'NEW\\r\\n'; sleep 3600"},
	}
	go func() { hostBErr <- hostB.Run(ctxHostB) }()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost(hostBID)
	}, hostBErr)

	waitUntilDebug(t, 15*time.Second, func() bool {
		active, err := readActiveSessionID(ctx)
		if err != nil || active != hostBID {
			return false
		}
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		info, _ := readViewInfo(ctx)
		active, _ := readActiveSessionID(ctx)
		debug, _ := readDebugInfo(ctx)
		return fmt.Sprintf("expected switch to new active session (active=%q ws=%.0f attempt=%d pending=%t debug=%s)",
			active, info.WSState, info.ReconnectAttempt, info.ReconnectPending, debug)
	}, hostBErr)
}

func TestWebUIManualRefreshButtonDiscoversSessions(t *testing.T) {
	clk := newTestClock()

	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	users := relay.NewUserStore()
	created, err := relay.CreateUser(users, "webuser", "pass", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	store := relay.NewStore()
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	baseURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	endpoint := baseURL + "/v1"

	code, err := totp.GenerateCodeCustom(created.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	ctx, cancel := newChromedpContext(t, filepath.Join(home, "chromedp-manual-refresh"))
	defer cancel()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("chromedp network enable: %v", err)
	}

	loginURL := endpoint + "/"
	if err := ensureWebUILogin(ctx, loginURL, created.User.Username, "pass", code); err != nil {
		t.Fatalf("chromedp login: %v", err)
	}
	waitForTerminalView(t, ctx, 15*time.Second)

	waitUntilDebug(t, 5*time.Second, func() bool {
		active, err := readActiveSessionID(ctx)
		return err == nil && active == ""
	}, func() string {
		active, _ := readActiveSessionID(ctx)
		debug, _ := readDebugInfo(ctx)
		return fmt.Sprintf("expected no active session before host start (active=%q debug=%s)", active, debug)
	})

	hostSessionID := "manual-refresh-host"
	access, err := store.CreateAccessToken(created.User.Username, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	ctxHost, cancelHost := context.WithCancel(context.Background())
	t.Cleanup(cancelHost)
	hostErr := make(chan error, 1)
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: hostSessionID,
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/sh", "-c", "printf 'READY\\r\\n'; sleep 3600"},
	}
	go func() { hostErr <- h.Run(ctxHost) }()

	waitUntil(t, clk, 5*time.Second, func() bool {
		return hub.HasHost(hostSessionID)
	}, hostErr)

	if err := chromedp.Run(ctx, chromedp.Click("#refresh-sessions-btn", chromedp.ByID)); err != nil {
		t.Fatalf("click refresh sessions: %v", err)
	}

	waitUntilDebug(t, 10*time.Second, func() bool {
		active, err := readActiveSessionID(ctx)
		if err != nil || active != hostSessionID {
			return false
		}
		info, err := readViewInfo(ctx)
		return err == nil && info.WSState == 1
	}, func() string {
		active, _ := readActiveSessionID(ctx)
		info, _ := readViewInfo(ctx)
		debug, _ := readDebugInfo(ctx)
		return fmt.Sprintf("expected manual refresh to discover/connect host (active=%q ws=%.0f attempt=%d pending=%t debug=%s)",
			active, info.WSState, info.ReconnectAttempt, info.ReconnectPending, debug)
	}, hostErr)
}

func hasCookie(cookies []*network.Cookie, name string) bool {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return true
		}
	}
	return false
}
