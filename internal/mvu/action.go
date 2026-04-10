package mvu

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/theme"
)

// ContextInput describes runtime context synchronization.
type ContextInput struct {
	Clock     clock.Clock
	Endpoint  string
	Theme     theme.TUITheme
	SessionID string
}

// SessionTabsInput describes a tab model synchronization request.
type SessionTabsInput struct {
	Endpoint string
	Sources  []SessionTabSource
	ActiveID string
	Options  BuildSessionTabsOptions
}

// WallInput describes wall overlay synchronization.
type WallInput struct {
	Visible  bool
	Title    string
	Message  string
	Duration time.Duration
}

// StatusKind identifies connection status transitions.
type StatusKind int

const (
	// StatusClear clears transient connection status banners.
	StatusClear StatusKind = iota
	// StatusConnected shows a temporary "connected" banner.
	StatusConnected
	// StatusError shows a temporary error banner.
	StatusError
	// StatusLoading shows a persistent loading banner.
	StatusLoading
	// StatusConnectionLost shows a persistent connection-lost banner.
	StatusConnectionLost
	// StatusConnectionBackoff shows a persistent reconnect countdown banner.
	StatusConnectionBackoff
)

// StatusInput describes a connection status synchronization request.
type StatusInput struct {
	Kind      StatusKind
	Endpoint  string
	Message   string
	Remaining time.Duration
	Duration  time.Duration
}

// AttachStatusInput describes an attach status transition.
type AttachStatusInput struct {
	Kind      StatusKind
	Endpoint  string
	Message   string
	Duration  time.Duration
	Now       time.Time
	Connected bool
}

// Action is a typed MVU command that mutates runtime state and returns effect hints.
type Action interface {
	run(*Runtime, time.Time) ActionResult
}

// ActionResult reports state transition + redraw/effect hints from one action.
type ActionResult struct {
	Changed   bool
	Delay     time.Duration
	ForceFull bool
	Visible   bool
	Tabs      []Tab
	Active    int
	Overlay   DisconnectedOverlayResult
	State     State
}

// ApplyAction executes one action against runtime state.
func (r *Runtime) ApplyAction(action Action) ActionResult {
	if r == nil || action == nil {
		return ActionResult{}
	}
	now := r.now()
	result := action.run(r, now)
	result.State = r.State()
	return result
}

// ContextAction synchronizes runtime context/bootstrap state.
type ContextAction struct {
	Input ContextInput
}

func (a ContextAction) run(r *Runtime, _ time.Time) ActionResult {
	before := r.State()
	if a.Input.Clock != nil {
		r.setClock(a.Input.Clock)
	}
	endpoint := strings.TrimSpace(a.Input.Endpoint)
	if endpoint != "" && endpoint != before.Endpoint {
		r.setEndpoint(endpoint)
	}
	if a.Input.SessionID != "" && a.Input.SessionID != before.SessionID {
		r.setSessionID(a.Input.SessionID)
	}
	if !reflect.DeepEqual(a.Input.Theme, theme.TUITheme{}) && !reflect.DeepEqual(a.Input.Theme, before.Theme) {
		r.setTheme(a.Input.Theme)
	}
	after := r.State()
	return ActionResult{Changed: !reflect.DeepEqual(before, after), State: after}
}

// SessionTabsAction synchronizes endpoint + tabs state.
type SessionTabsAction struct {
	Input SessionTabsInput
}

func (a SessionTabsAction) run(r *Runtime, _ time.Time) ActionResult {
	before := r.State()
	if endpoint := strings.TrimSpace(a.Input.Endpoint); endpoint != "" && endpoint != before.Endpoint {
		r.setEndpoint(endpoint)
	}
	tabs, active := r.setTabsFromSources(a.Input.Sources, a.Input.ActiveID, a.Input.Options)
	after := r.State()
	return ActionResult{
		Changed: !sessionTabsStateEqual(before, after),
		Tabs:    tabs,
		Active:  active,
		State:   after,
	}
}

func sessionTabsStateEqual(a, b State) bool {
	if a.Endpoint != b.Endpoint || a.ActiveTab != b.ActiveTab || a.SessionID != b.SessionID {
		return false
	}
	if len(a.Tabs) != len(b.Tabs) {
		return false
	}
	for i := range a.Tabs {
		if a.Tabs[i] != b.Tabs[i] {
			return false
		}
	}
	return true
}

// TabWakeAction wakes tab visibility timers.
type TabWakeAction struct {
	Duration time.Duration
}

func (a TabWakeAction) run(r *Runtime, _ time.Time) ActionResult {
	before := r.State()
	if !before.TabBarVisible || len(before.Tabs) == 0 {
		return ActionResult{Changed: false, State: before}
	}
	r.wakeTabs(a.Duration)
	after := r.State()
	changed := !before.TabBarShownAt.Equal(after.TabBarShownAt) ||
		!before.TabBarExpiresAt.Equal(after.TabBarExpiresAt) ||
		before.TabBarVisible != after.TabBarVisible
	return ActionResult{Changed: changed, State: after}
}

// TabToggleAction toggles tab visibility.
type TabToggleAction struct{}

func (a TabToggleAction) run(r *Runtime, _ time.Time) ActionResult {
	before := r.State().TabBarVisible
	visible := r.toggleTabBar()
	return ActionResult{Changed: before != visible, Visible: visible, State: r.State()}
}

// TabSuppressedAction synchronizes tab suppression.
type TabSuppressedAction struct {
	Suppressed bool
}

func (a TabSuppressedAction) run(r *Runtime, _ time.Time) ActionResult {
	changed := r.setTabBarSuppressed(a.Suppressed)
	return ActionResult{Changed: changed, State: r.State()}
}

// HelpVisibleAction synchronizes help visibility.
type HelpVisibleAction struct {
	Visible bool
}

func (a HelpVisibleAction) run(r *Runtime, _ time.Time) ActionResult {
	before := r.State().HelpVisible
	if a.Visible {
		r.showHelp()
	} else {
		r.hideHelp()
	}
	after := r.State().HelpVisible
	return ActionResult{Changed: before != after, State: r.State()}
}

// ScrollbackPercentAction synchronizes row-1 scrollback status.
type ScrollbackPercentAction struct {
	Visible bool
	Percent int
}

func (a ScrollbackPercentAction) run(r *Runtime, _ time.Time) ActionResult {
	percent := a.Percent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	before := r.State().ScrollbackMessage
	if !a.Visible {
		r.clearScrollback()
		return ActionResult{Changed: before != "", State: r.State()}
	}
	msg := fmt.Sprintf("[%d%%]", percent)
	r.setScrollbackMessage(msg)
	return ActionResult{Changed: before != msg, State: r.State()}
}

// StatusAction synchronizes connection status and effect timings.
type StatusAction struct {
	Input StatusInput
}

func (a StatusAction) run(r *Runtime, now time.Time) ActionResult {
	changed := applyStatus(r, a.Input)
	return actionEffectFrom(r, changed, now)
}

// AttachStatusAction synchronizes attach connectivity + status with effect timings.
type AttachStatusAction struct {
	Input AttachStatusInput
}

func (a AttachStatusAction) run(r *Runtime, now time.Time) ActionResult {
	in := a.Input
	if in.Now.IsZero() {
		in.Now = now
	}
	overlay := r.applyAttachConnectivity(AttachConnectivityInput{
		Connected:          in.Connected,
		ConnectedOnce:      false,
		ReconnectAt:        time.Time{},
		WaitingForSessions: false,
		Endpoint:           in.Endpoint,
		Now:                in.Now,
	})
	statusChanged := applyStatus(r, StatusInput{
		Kind:     in.Kind,
		Endpoint: in.Endpoint,
		Message:  in.Message,
		Duration: in.Duration,
	})
	result := actionEffectFrom(r, statusChanged || overlay.Changed, in.Now)
	result.Overlay = overlay.Overlay
	return result
}

// AttachConnectivityAction synchronizes attach disconnected overlay state only.
type AttachConnectivityAction struct {
	Input AttachConnectivityInput
}

func (a AttachConnectivityAction) run(r *Runtime, now time.Time) ActionResult {
	in := a.Input
	if in.Now.IsZero() {
		in.Now = now
	}
	res := r.applyAttachConnectivity(in)
	return ActionResult{
		Changed: res.Changed,
		Overlay: res.Overlay,
		State:   r.State(),
	}
}

// DisconnectedOverlayAction synchronizes the disconnect overlay model.
type DisconnectedOverlayAction struct {
	Input DisconnectedOverlayInput
}

func (a DisconnectedOverlayAction) run(r *Runtime, now time.Time) ActionResult {
	in := a.Input
	if in.Now.IsZero() {
		in.Now = now
	}
	res := r.applyDisconnectedOverlay(in)
	return ActionResult{
		Changed: res.Changed,
		Overlay: res,
		State:   r.State(),
	}
}

// WallAction synchronizes wall overlay and effect timings.
type WallAction struct {
	Input WallInput
}

func (a WallAction) run(r *Runtime, now time.Time) ActionResult {
	before := r.State()
	title := strings.TrimSpace(a.Input.Title)
	if !a.Input.Visible || title == "" {
		r.hideWall()
	} else {
		r.showWall(title, strings.TrimSpace(a.Input.Message), a.Input.Duration)
	}
	after := r.State()
	changed := before.WallVisible != after.WallVisible ||
		before.WallTitle != after.WallTitle ||
		before.WallMessage != after.WallMessage ||
		!before.WallExpiresAt.Equal(after.WallExpiresAt)
	return actionEffectFrom(r, changed, now)
}

// ClearOverlaysAction clears all overlays.
type ClearOverlaysAction struct{}

func (a ClearOverlaysAction) run(r *Runtime, _ time.Time) ActionResult {
	if !r.HasActiveLayers() {
		return ActionResult{Changed: false, State: r.State()}
	}
	r.clearAllOverlays()
	return ActionResult{Changed: true, State: r.State()}
}

func applyStatus(r *Runtime, in StatusInput) bool {
	before := r.State()
	msg := strings.TrimSpace(in.Message)
	switch in.Kind {
	case StatusClear:
		r.hideConnection()
	case StatusConnected:
		if msg == "" {
			msg = ConnectedToMessage(in.Endpoint)
		}
		r.showConnected(msg, in.Duration)
	case StatusError:
		if msg == "" {
			msg = "temporary error"
		}
		r.showError(msg, in.Duration)
	case StatusLoading:
		if msg == "" {
			r.hideLoading()
			break
		}
		r.showLoading(msg)
	case StatusConnectionLost:
		if msg == "" {
			msg = ConnectionLostMessage(in.Endpoint)
		}
		r.showConnectionLost(msg)
	case StatusConnectionBackoff:
		if msg == "" {
			msg = ConnectionLostBackoffMessage(in.Endpoint, in.Remaining)
		}
		r.showConnectionLost(msg)
	default:
		return false
	}
	after := r.State()
	return before.ConnectionMessage != after.ConnectionMessage ||
		before.ConnectionStyle != after.ConnectionStyle ||
		!before.ConnectionShownAt.Equal(after.ConnectionShownAt) ||
		!before.ConnectionExpiresAt.Equal(after.ConnectionExpiresAt) ||
		before.LoadingMessage != after.LoadingMessage
}

func actionEffectFrom(r *Runtime, changed bool, now time.Time) ActionResult {
	if now.IsZero() {
		now = r.now()
	}
	state := r.State()
	return ActionResult{
		Changed:   changed,
		Delay:     r.ExpiryDelay(now),
		ForceFull: state.DisconnectVisible,
		State:     state,
	}
}
