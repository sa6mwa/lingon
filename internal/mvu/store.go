package mvu

import (
	"sync"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/theme"
)

// Runtime owns MVU model state and effect timing for terminal overlays.
type Runtime struct {
	mu                   sync.Mutex
	state                State
	clock                clock.Clock
	tabBarCursorTopSince time.Time
	tabBarAutoHidden     bool
	tabBarSuppressed     bool
	tabBarSuppressedPrev bool
}

var tabBarAutoHideDelay = 120 * time.Millisecond

// SetTabBarAutoHideDelay overrides the auto-hide delay and returns restore fn.
func SetTabBarAutoHideDelay(d time.Duration) func() {
	prev := tabBarAutoHideDelay
	tabBarAutoHideDelay = d
	return func() {
		tabBarAutoHideDelay = prev
	}
}

// TabBarAutoHideDuration returns the current tab auto-hide delay.
func TabBarAutoHideDuration() time.Duration {
	return tabBarAutoHideDelay
}

// NewRuntime constructs a new MVU runtime store.
func NewRuntime() *Runtime {
	return &Runtime{
		clock: clock.New(),
		state: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
		},
	}
}

// setClock overrides the clock used for time-based state.
func (r *Runtime) setClock(clk clock.Clock) {
	if r == nil || clk == nil {
		return
	}
	r.mu.Lock()
	r.clock = clk
	r.mu.Unlock()
}

func (r *Runtime) nowLocked() time.Time {
	if r == nil || r.clock == nil {
		return time.Now()
	}
	return r.clock.Now()
}

func (r *Runtime) mutateState(fn func(state *State, now time.Time)) {
	if r == nil || fn == nil {
		return
	}
	r.mu.Lock()
	now := r.nowLocked()
	expireState(&r.state, now)
	fn(&r.state, now)
	expireState(&r.state, now)
	r.mu.Unlock()
}

// State returns a snapshot of current runtime state with expirations resolved.
func (r *Runtime) State() State {
	state, _ := r.snapshotState()
	return state
}

// setSessionID updates the active session identifier.
func (r *Runtime) setSessionID(sessionID string) {
	r.mutateState(func(state *State, _ time.Time) {
		state.SessionID = sessionID
	})
}

// setEndpoint updates the endpoint label used in overlays.
func (r *Runtime) setEndpoint(endpoint string) {
	r.mutateState(func(state *State, _ time.Time) {
		state.Endpoint = endpoint
	})
}

// setTheme updates the active UI theme.
func (r *Runtime) setTheme(t theme.TUITheme) {
	r.mutateState(func(state *State, _ time.Time) {
		state.Theme = t
	})
}

// setTabs replaces the tab model and active index.
func (r *Runtime) setTabs(tabs []Tab, active int, sessionID string) {
	r.mutateState(func(state *State, _ time.Time) {
		state.Tabs = append([]Tab(nil), tabs...)
		state.ActiveTab = clampActiveTab(len(state.Tabs), active)
		if sessionID != "" {
			state.SessionID = sessionID
		}
	})
}

// setTabsFromSources builds tabs from session sources and stores them in runtime state.
func (r *Runtime) setTabsFromSources(sources []SessionTabSource, activeID string, opts BuildSessionTabsOptions) ([]Tab, int) {
	tabs, active := BuildSessionTabs(sources, activeID, opts)
	r.setTabs(tabs, active, activeID)
	return tabs, active
}

// toggleTabBar toggles visibility and clears timers.
func (r *Runtime) toggleTabBar() bool {
	if r == nil {
		return false
	}
	visible := false
	r.mutateState(func(state *State, now time.Time) {
		if r.tabBarSuppressed {
			r.tabBarSuppressedPrev = !r.tabBarSuppressedPrev
			visible = r.tabBarSuppressedPrev
			state.TabBarVisible = false
			state.TabBarShownAt = time.Time{}
			state.TabBarExpiresAt = time.Time{}
			r.tabBarAutoHidden = false
			r.tabBarCursorTopSince = time.Time{}
			return
		}
		state.TabBarVisible = !state.TabBarVisible
		visible = state.TabBarVisible
		if state.TabBarVisible {
			state.TabBarShownAt = now
		} else {
			state.TabBarShownAt = time.Time{}
			r.tabBarAutoHidden = false
			r.tabBarCursorTopSince = time.Time{}
		}
		state.TabBarExpiresAt = time.Time{}
	})
	return visible
}

// setTabBarSuppressed applies temporary tab suppression and restores prior visibility.
func (r *Runtime) setTabBarSuppressed(suppressed bool) bool {
	if r == nil {
		return false
	}
	changed := false
	r.mutateState(func(state *State, now time.Time) {
		if suppressed {
			if r.tabBarSuppressed {
				return
			}
			r.tabBarSuppressed = true
			r.tabBarSuppressedPrev = state.TabBarVisible
			if state.TabBarVisible {
				changed = true
			}
			state.TabBarVisible = false
			state.TabBarShownAt = time.Time{}
			state.TabBarExpiresAt = time.Time{}
			r.tabBarAutoHidden = false
			r.tabBarCursorTopSince = time.Time{}
			return
		}
		if !r.tabBarSuppressed {
			return
		}
		r.tabBarSuppressed = false
		restore := r.tabBarSuppressedPrev
		r.tabBarSuppressedPrev = false
		if state.TabBarVisible != restore {
			changed = true
		}
		state.TabBarVisible = restore
		if restore {
			state.TabBarShownAt = now
		} else {
			state.TabBarShownAt = time.Time{}
		}
		state.TabBarExpiresAt = time.Time{}
		r.tabBarAutoHidden = false
		r.tabBarCursorTopSince = time.Time{}
	})
	return changed
}

// setScrollbackMessage sets the top-row scrollback indicator message.
func (r *Runtime) setScrollbackMessage(message string) {
	r.mutateState(func(state *State, _ time.Time) {
		state.ScrollbackMessage = message
	})
}

// clearScrollback removes scrollback indicator message.
func (r *Runtime) clearScrollback() {
	r.setScrollbackMessage("")
}

// clearAllOverlays clears all overlays and tab bar visibility.
func (r *Runtime) clearAllOverlays() {
	r.mutateState(func(state *State, _ time.Time) {
		state.HelpVisible = false
		state.ConnectionMessage = ""
		state.ConnectionStyle = BannerRed
		state.ConnectionShownAt = time.Time{}
		state.ConnectionExpiresAt = time.Time{}
		state.LoadingMessage = ""
		state.DisconnectTitle = ""
		state.DisconnectDetail = ""
		state.DisconnectVisible = false
		state.DisconnectBoxWidth = 0
		state.WallTitle = ""
		state.WallMessage = ""
		state.WallVisible = false
		state.WallExpiresAt = time.Time{}
		state.ScrollbackMessage = ""
		state.TabBarVisible = false
		state.TabBarShownAt = time.Time{}
		state.TabBarExpiresAt = time.Time{}
		r.tabBarSuppressed = false
		r.tabBarSuppressedPrev = false
		r.tabBarAutoHidden = false
		r.tabBarCursorTopSince = time.Time{}
	})
}

// HelpVisible reports whether the help overlay is visible.
func (r *Runtime) HelpVisible() bool {
	return r.State().HelpVisible
}

// showHelp enables the help overlay.
func (r *Runtime) showHelp() {
	r.mutateState(func(state *State, _ time.Time) {
		state.HelpVisible = true
	})
}

// hideHelp disables the help overlay.
func (r *Runtime) hideHelp() {
	r.mutateState(func(state *State, _ time.Time) {
		state.HelpVisible = false
	})
}

// showConnectionLost displays a persistent red connection banner.
func (r *Runtime) showConnectionLost(message string) {
	if message == "" {
		return
	}
	r.mutateState(func(state *State, now time.Time) {
		state.ConnectionMessage = message
		state.ConnectionStyle = BannerRed
		state.ConnectionShownAt = now
		state.ConnectionExpiresAt = time.Time{}
	})
}

// showConnected displays a temporary green connection banner.
func (r *Runtime) showConnected(message string, d time.Duration) {
	if message == "" {
		return
	}
	r.mutateState(func(state *State, now time.Time) {
		state.ConnectionMessage = message
		state.ConnectionStyle = BannerGreen
		state.ConnectionShownAt = now
		if d > 0 {
			state.ConnectionExpiresAt = now.Add(d)
		} else {
			state.ConnectionExpiresAt = time.Time{}
		}
	})
}

// showError displays a temporary red connection banner.
func (r *Runtime) showError(message string, d time.Duration) {
	if message == "" {
		return
	}
	r.mutateState(func(state *State, now time.Time) {
		state.ConnectionMessage = message
		state.ConnectionStyle = BannerRed
		state.ConnectionShownAt = now
		if d > 0 {
			state.ConnectionExpiresAt = now.Add(d)
		} else {
			state.ConnectionExpiresAt = time.Time{}
		}
	})
}

// hideConnection clears the connection banner.
func (r *Runtime) hideConnection() {
	r.mutateState(func(state *State, _ time.Time) {
		state.ConnectionMessage = ""
		state.ConnectionStyle = BannerRed
		state.ConnectionShownAt = time.Time{}
		state.ConnectionExpiresAt = time.Time{}
	})
}

// showLoading displays a persistent loading banner.
func (r *Runtime) showLoading(message string) {
	if message == "" {
		return
	}
	r.mutateState(func(state *State, _ time.Time) {
		state.LoadingMessage = message
	})
}

// hideLoading clears the loading banner.
func (r *Runtime) hideLoading() {
	r.mutateState(func(state *State, _ time.Time) {
		state.LoadingMessage = ""
	})
}

// showDisconnected displays a persistent disconnect dialog.
func (r *Runtime) showDisconnected(title, detail string) {
	if title == "" {
		return
	}
	r.mutateState(func(state *State, _ time.Time) {
		lines := []string{title}
		if detail != "" {
			lines = append(lines, "", detail)
		}
		state.DisconnectTitle = title
		state.DisconnectDetail = detail
		state.DisconnectVisible = true
		state.DisconnectBoxWidth = helpBoxWidth(lines)
	})
}

// hideDisconnected clears the disconnect dialog.
func (r *Runtime) hideDisconnected() {
	r.mutateState(func(state *State, _ time.Time) {
		state.DisconnectTitle = ""
		state.DisconnectDetail = ""
		state.DisconnectVisible = false
		state.DisconnectBoxWidth = 0
	})
}

// DisconnectedVisible reports whether the disconnect dialog is visible.
func (r *Runtime) DisconnectedVisible() bool {
	state := r.State()
	return state.DisconnectVisible && state.DisconnectTitle != ""
}

// showWall displays a centered non-blocking wall notification.
func (r *Runtime) showWall(title, message string, d time.Duration) {
	if title == "" {
		return
	}
	r.mutateState(func(state *State, now time.Time) {
		state.WallTitle = title
		state.WallMessage = message
		state.WallVisible = true
		if d > 0 {
			state.WallExpiresAt = now.Add(d)
		} else {
			state.WallExpiresAt = time.Time{}
		}
	})
}

// hideWall clears wall notification.
func (r *Runtime) hideWall() {
	r.mutateState(func(state *State, _ time.Time) {
		state.WallTitle = ""
		state.WallMessage = ""
		state.WallVisible = false
		state.WallExpiresAt = time.Time{}
	})
}

// wakeTabs marks tabs visible and resets auto-hide tracking.
func (r *Runtime) wakeTabs(d time.Duration) {
	r.mutateState(func(state *State, now time.Time) {
		r.tabBarCursorTopSince = time.Time{}
		r.tabBarAutoHidden = false
		if r.tabBarSuppressed {
			r.tabBarSuppressedPrev = true
			state.TabBarVisible = false
			state.TabBarShownAt = time.Time{}
			state.TabBarExpiresAt = time.Time{}
			return
		}
		state.TabBarVisible = true
		state.TabBarShownAt = now
		if d > 0 {
			state.TabBarExpiresAt = now.Add(d)
		} else {
			state.TabBarExpiresAt = time.Time{}
		}
	})
}

// TabBarVisibility resolves tab visibility for cursor position and active banners.
func (r *Runtime) TabBarVisibility(state State, cursor Cursor, now time.Time) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !state.TabBarVisible || len(state.Tabs) == 0 {
		r.tabBarCursorTopSince = time.Time{}
		r.tabBarAutoHidden = false
		return false, 0
	}
	if cursor.Row <= 1 && state.ConnectionMessage == "" && state.LoadingMessage == "" && state.ScrollbackMessage == "" {
		r.tabBarCursorTopSince = now
		r.tabBarAutoHidden = true
		return false, 0
	}
	r.tabBarCursorTopSince = time.Time{}
	r.tabBarAutoHidden = false
	return true, 0
}

// TabBarAutoHideDelay reports remaining delay before auto-hide triggers.
func (r *Runtime) TabBarAutoHideDelay(now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tabBarAutoHidden || r.tabBarCursorTopSince.IsZero() {
		return 0
	}
	elapsed := now.Sub(r.tabBarCursorTopSince)
	delay := tabBarAutoHideDelay
	if elapsed >= delay {
		return 0
	}
	return delay - elapsed
}

// HasActiveLayers reports whether any overlay should currently be rendered.
func (r *Runtime) HasActiveLayers() bool {
	state, now := r.snapshotState()
	resolved := Resolve(state, Cursor{Row: 2, Col: 1}, now, ResolveOptions{})
	return resolved.TopOverlayVisible || resolved.FullOverlayVisible
}

// Compose overlays visible layers on top of base output using runtime state.
func (r *Runtime) Compose(base []byte, cols, rows int, baseCursor Cursor) []byte {
	state, now := r.snapshotState()
	resolved := Resolve(state, baseCursor, now, ResolveOptions{})
	return ComposeResolved(base, cols, rows, baseCursor, resolved)
}

// ExpiryDelay reports the next overlay/banner expiry delay from current runtime state.
func (r *Runtime) ExpiryDelay(now time.Time) time.Duration {
	if r == nil {
		return 0
	}
	if now.IsZero() {
		now = r.now()
	}
	state := r.State()
	return NextExpiryDelay(state, now)
}

func (r *Runtime) now() time.Time {
	r.mu.Lock()
	clk := r.clock
	r.mu.Unlock()
	if clk == nil {
		return time.Now()
	}
	return clk.Now()
}

func (r *Runtime) snapshotState() (State, time.Time) {
	now := r.now()
	r.mu.Lock()
	state := r.state
	expireState(&state, now)
	r.state = state
	r.mu.Unlock()
	return state, now
}
