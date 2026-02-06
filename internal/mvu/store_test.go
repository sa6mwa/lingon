package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/theme"
)

func newRuntimeMockClock() (*Runtime, *clock.MockClock) {
	rt := NewRuntime()
	mock := clock.NewMock()
	rt.ApplyAction(ContextAction{Input: ContextInput{Clock: mock}})
	return rt, mock
}

func setTabsFromNames(rt *Runtime, activeID string, names ...string) ActionResult {
	sources := make([]SessionTabSource, 0, len(names))
	for _, name := range names {
		sources = append(sources, SessionTabSource{ID: "tab-" + name, Name: name})
	}
	return rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: sources, ActiveID: activeID}})
}

func TestRuntimeContextAndThemeTransitions(t *testing.T) {
	rt, _ := newRuntimeMockClock()
	res := rt.ApplyAction(ContextAction{Input: ContextInput{
		SessionID: "session-a",
		Endpoint:  "https://relay.example/v1",
		Theme:     theme.TUI("default"),
	}})
	if !res.Changed {
		t.Fatalf("expected context action to report changed")
	}
	state := rt.State()
	if state.SessionID != "session-a" {
		t.Fatalf("expected session id, got %q", state.SessionID)
	}
	if state.Endpoint != "https://relay.example/v1" {
		t.Fatalf("expected endpoint, got %q", state.Endpoint)
	}
	if state.Theme.TabBg == "" {
		t.Fatalf("expected theme to be initialized")
	}
}

func TestRuntimeSessionTabsFromSourcesBuildsState(t *testing.T) {
	rt := NewRuntime()
	res := rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Sources: []SessionTabSource{
			{ID: "local-1", Name: "local"},
			{ID: "remote-1", Name: "remote"},
		},
		ActiveID: "remote-1",
		Options: BuildSessionTabsOptions{
			LocalIDs: map[string]bool{"local-1": true},
			Disabled: map[string]bool{"remote-1": true},
			Muted:    map[string]bool{"local-1": true},
		},
	}})
	if !res.Changed {
		t.Fatalf("expected tabs action changed")
	}
	if len(res.Tabs) != 2 || res.Active != 1 {
		t.Fatalf("unexpected tabs result: len=%d active=%d", len(res.Tabs), res.Active)
	}
	state := rt.State()
	if state.Tabs[0].Title != "*local" {
		t.Fatalf("tabs[0].title=%q, want %q", state.Tabs[0].Title, "*local")
	}
	if !state.Tabs[0].Muted {
		t.Fatalf("tabs[0].muted=false, want true")
	}
	if !state.Tabs[1].Disabled {
		t.Fatalf("tabs[1].disabled=false, want true")
	}
	if state.ActiveTab != 1 {
		t.Fatalf("active tab=%d, want 1", state.ActiveTab)
	}
}

func TestRuntimeTabOpsAndExpiry(t *testing.T) {
	rt, mock := newRuntimeMockClock()
	setTabsFromNames(rt, "tab-a", "a")

	wake := rt.ApplyAction(TabWakeAction{Duration: 3 * time.Second})
	if !wake.Changed {
		t.Fatalf("expected tab wake changed")
	}
	state := rt.State()
	if !state.TabBarVisible {
		t.Fatalf("expected tabs visible after wake")
	}
	if state.TabBarShownAt.IsZero() || state.TabBarExpiresAt.IsZero() {
		t.Fatalf("expected wake timestamps to be set")
	}
	mock.Add(3500 * time.Millisecond)
	if rt.State().TabBarVisible {
		t.Fatalf("expected tab wake timeout to expire")
	}

	toggle := rt.ApplyAction(TabToggleAction{})
	if !toggle.Visible {
		t.Fatalf("expected toggle to show tab bar")
	}
	if !rt.State().TabBarVisible {
		t.Fatalf("expected tab bar visible")
	}
	if !rt.ApplyAction(TabSuppressedAction{Suppressed: true}).Changed {
		t.Fatalf("expected suppression to hide tab bar")
	}
	if rt.State().TabBarVisible {
		t.Fatalf("expected tab bar hidden when suppressed")
	}
}

func TestRuntimeOverlayAndBannerTransitions(t *testing.T) {
	rt, _ := newRuntimeMockClock()
	if !rt.ApplyAction(HelpVisibleAction{Visible: true}).Changed {
		t.Fatalf("expected help visible change")
	}
	if !rt.ApplyAction(ScrollbackPercentAction{Visible: true, Percent: 73}).Changed {
		t.Fatalf("expected scrollback visible change")
	}
	status := rt.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnectionLost, Message: "relay unavailable"}})
	if !status.Changed {
		t.Fatalf("expected status changed")
	}
	if status.State.ConnectionStyle != BannerRed {
		t.Fatalf("expected red style")
	}
	wall := rt.ApplyAction(WallAction{Input: WallInput{Visible: true, Title: "Broadcast:", Message: "hello", Duration: 2 * time.Second}})
	if !wall.Changed || wall.ForceFull {
		t.Fatalf("expected wall changed without full redraw hint")
	}

	clear := rt.ApplyAction(ClearOverlaysAction{})
	if !clear.Changed {
		t.Fatalf("expected clear overlays changed")
	}
	state := rt.State()
	if state.HelpVisible || state.ConnectionMessage != "" || state.WallVisible || state.ScrollbackMessage != "" {
		t.Fatalf("expected overlays cleared, got %+v", state)
	}
}

func TestRuntimeStatusFormattingAndExpiry(t *testing.T) {
	rt, mock := newRuntimeMockClock()
	connected := rt.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnected, Endpoint: "https://relay.example/v1", Duration: 2 * time.Second}})
	if !connected.Changed {
		t.Fatalf("expected connected status changed")
	}
	if !strings.Contains(connected.State.ConnectionMessage, "connected to https://relay.example/v1") {
		t.Fatalf("unexpected connected message: %q", connected.State.ConnectionMessage)
	}
	if connected.State.ConnectionStyle != BannerGreen {
		t.Fatalf("expected green style")
	}
	if connected.Delay <= 0 {
		t.Fatalf("expected positive status delay")
	}
	mock.Add(2500 * time.Millisecond)
	if rt.State().ConnectionMessage != "" {
		t.Fatalf("expected connected banner to expire")
	}

	backoff := rt.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnectionBackoff, Endpoint: "https://relay.example/v1", Remaining: 1500 * time.Millisecond}})
	if !backoff.Changed {
		t.Fatalf("expected backoff status changed")
	}
	if !strings.Contains(backoff.State.ConnectionMessage, "reconnecting in 2s") {
		t.Fatalf("unexpected backoff message: %q", backoff.State.ConnectionMessage)
	}
}

func TestRuntimeDisconnectedTransitionsViaActions(t *testing.T) {
	rt := NewRuntime()
	now := time.Unix(5000, 0)

	overlay := rt.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(2 * time.Second),
		Now:           now,
	}})
	if !overlay.Changed || !overlay.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	if overlay.Overlay.DisconnectTitle != "Not connected" {
		t.Fatalf("disconnect title=%q", overlay.Overlay.DisconnectTitle)
	}

	attach := rt.ApplyAction(AttachConnectivityAction{Input: AttachConnectivityInput{Connected: true, Now: now.Add(time.Second)}})
	if !attach.Changed {
		t.Fatalf("expected attach connectivity change")
	}
	state := rt.State()
	if state.DisconnectVisible {
		t.Fatalf("expected disconnect overlay hidden")
	}
	if state.ConnectionMessage != "" {
		t.Fatalf("expected connection banner cleared")
	}
}

func TestRuntimeTabBarVisibilityRules(t *testing.T) {
	rt, mock := newRuntimeMockClock()
	setTabsFromNames(rt, "tab-a", "a")

	base := rt.State()
	visible, _ := rt.TabBarVisibility(base, Cursor{Row: 2, Col: 1, Visible: true}, mock.Now())
	if !visible {
		t.Fatalf("expected tabs visible when cursor is below top row")
	}

	visible, _ = rt.TabBarVisibility(base, Cursor{Row: 1, Col: 4, Visible: true}, mock.Now())
	if visible {
		t.Fatalf("expected tabs hidden when cursor owns top row and no banner")
	}

	withBanner := base
	withBanner.ConnectionMessage = "connection lost"
	withBanner.ConnectionShownAt = mock.Now()
	visible, _ = rt.TabBarVisibility(withBanner, Cursor{Row: 1, Col: 4, Visible: true}, mock.Now())
	if !visible {
		t.Fatalf("expected tabs visible when banner is active on top row")
	}

	noTabs := base
	noTabs.Tabs = nil
	visible, _ = rt.TabBarVisibility(noTabs, Cursor{Row: 2, Col: 1, Visible: true}, mock.Now())
	if visible {
		t.Fatalf("expected tabs hidden when no tabs are present")
	}
}

func TestRuntimeHasActiveLayersSemantics(t *testing.T) {
	rt, _ := newRuntimeMockClock()
	if rt.HasActiveLayers() {
		t.Fatalf("expected no active layers initially")
	}

	setTabsFromNames(rt, "tab-a", "a")
	if !rt.HasActiveLayers() {
		t.Fatalf("expected active top layer with visible tabs")
	}

	rt.ApplyAction(TabSuppressedAction{Suppressed: true})
	rt.ApplyAction(HelpVisibleAction{Visible: true})
	if !rt.HasActiveLayers() {
		t.Fatalf("expected active full overlay with help")
	}

	rt.ApplyAction(ClearOverlaysAction{})
	if rt.HasActiveLayers() {
		t.Fatalf("expected layers cleared")
	}
}

func TestRuntimeExpiryDelay(t *testing.T) {
	mock := clock.NewMock()
	rt := NewRuntime()
	rt.ApplyAction(ContextAction{Input: ContextInput{Clock: mock}})
	rt.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnected, Message: "connected", Duration: 3 * time.Second}})
	if got := rt.ExpiryDelay(mock.Now()); got != 3*time.Second {
		t.Fatalf("expiry delay=%v, want 3s", got)
	}
	if got := rt.ExpiryDelay(mock.Now().Add(time.Second)); got != 2*time.Second {
		t.Fatalf("expiry delay after 1s=%v, want 2s", got)
	}
}

func TestRuntimeComposeAndTopOverlayRendering(t *testing.T) {
	rt, _ := newRuntimeMockClock()
	const cols, rows = 80, 8
	base := []byte("\x1b[2;1Hpayload")
	setTabsFromNames(rt, "tab-a", "tab-a")
	rt.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnected, Message: "connected", Duration: 2 * time.Second}})

	out := rt.Compose(base, cols, rows, Cursor{Row: 2, Col: 10, Visible: true})
	if !strings.Contains(string(out), "tab-a") {
		t.Fatalf("expected composed frame to include tab title under banner overlay")
	}
	if !strings.Contains(string(out), "connected") {
		t.Fatalf("expected composed frame to include connected banner")
	}
	if !strings.Contains(string(out), "\x1b[2;10H") {
		t.Fatalf("expected cursor restore sequence in composed output")
	}
}

func TestRuntimeTopOverlayRender(t *testing.T) {
	rt, _ := newRuntimeMockClock()
	resolved := Resolve(rt.State(), Cursor{Row: 2, Col: 2, Visible: true}, time.Now(), ResolveOptions{ForceTabsVisible: true})
	out := ComposeTopOverlayResolved(80, Cursor{Row: 2, Col: 2, Visible: true}, resolved)
	if strings.Contains(string(out), "tab-a") {
		t.Fatalf("expected no tab bar content when tabs are not configured")
	}

	setTabsFromNames(rt, "tab-a", "tab-a")
	resolved = Resolve(rt.State(), Cursor{Row: 2, Col: 3, Visible: true}, time.Now(), ResolveOptions{ForceTabsVisible: true})
	out = ComposeTopOverlayResolved(80, Cursor{Row: 2, Col: 3, Visible: true}, resolved)
	if len(out) == 0 {
		t.Fatalf("expected tab bar overlay bytes")
	}
	if !strings.Contains(string(out), "\x1b[1;1H") {
		t.Fatalf("expected tab overlay to target row 1")
	}
	if !strings.Contains(string(out), "tab-a") {
		t.Fatalf("expected tab title in tab overlay output")
	}
}
