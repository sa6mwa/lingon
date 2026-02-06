package mvu

import (
	"strings"
	"testing"
	"time"
)

func TestSessionTabsActionUpdatesEndpointAndTabs(t *testing.T) {
	rt := NewRuntime()
	res := rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Endpoint: "https://relay.example/v1",
		Sources: []SessionTabSource{
			{ID: "a", Name: "alpha"},
			{ID: "b", Name: "beta"},
		},
		ActiveID: "b",
	}})
	if !res.Changed {
		t.Fatalf("expected tabs action to report changed")
	}
	if len(res.Tabs) != 2 || res.Active != 1 {
		t.Fatalf("unexpected tabs result: len=%d active=%d", len(res.Tabs), res.Active)
	}
	state := rt.State()
	if state.Endpoint != "https://relay.example/v1" {
		t.Fatalf("endpoint=%q", state.Endpoint)
	}
	if state.ActiveTab != 1 {
		t.Fatalf("active_tab=%d", state.ActiveTab)
	}
}

func TestHelpAndTabActions(t *testing.T) {
	rt := NewRuntime()
	rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "a", Name: "alpha"}}, ActiveID: "a"}})

	if !rt.ApplyAction(HelpVisibleAction{Visible: true}).Changed {
		t.Fatalf("expected show help changed")
	}
	if !rt.State().HelpVisible {
		t.Fatalf("help should be visible")
	}
	if !rt.ApplyAction(HelpVisibleAction{Visible: false}).Changed {
		t.Fatalf("expected hide help changed")
	}
	if rt.State().HelpVisible {
		t.Fatalf("help should be hidden")
	}

	if !rt.ApplyAction(TabSuppressedAction{Suppressed: true}).Changed {
		t.Fatalf("expected suppression to change visibility")
	}
	if rt.State().TabBarVisible {
		t.Fatalf("tab bar should be hidden when suppressed")
	}
	if !rt.ApplyAction(TabSuppressedAction{Suppressed: false}).Changed {
		t.Fatalf("expected unsuppress to change visibility")
	}
	if !rt.State().TabBarVisible {
		t.Fatalf("tab bar should be visible when unsuppressed")
	}

	toggle := rt.ApplyAction(TabToggleAction{})
	if toggle.Visible {
		t.Fatalf("tab toggle expected hidden state")
	}
	if !rt.ApplyAction(TabToggleAction{}).Visible {
		t.Fatalf("tab toggle expected visible state")
	}
	if !rt.ApplyAction(TabWakeAction{Duration: 2 * time.Second}).Changed {
		t.Fatalf("expected wake to update tab timing")
	}
	if rt.State().TabBarShownAt.IsZero() {
		t.Fatalf("expected wake to update shown_at")
	}
}

func TestWallAndScrollbackActions(t *testing.T) {
	rt := NewRuntime()
	if !rt.ApplyAction(WallAction{Input: WallInput{Visible: true, Title: "Broadcast:", Message: "hello", Duration: 2 * time.Second}}).Changed {
		t.Fatalf("expected wall show changed")
	}
	state := rt.State()
	if !state.WallVisible || state.WallTitle != "Broadcast:" || state.WallMessage != "hello" {
		t.Fatalf("unexpected wall state: %+v", state)
	}
	if !rt.ApplyAction(WallAction{Input: WallInput{Visible: false}}).Changed {
		t.Fatalf("expected wall hide changed")
	}
	if rt.State().WallVisible {
		t.Fatalf("wall should be hidden")
	}

	if !rt.ApplyAction(ScrollbackPercentAction{Visible: true, Percent: 44}).Changed {
		t.Fatalf("expected scrollback show changed")
	}
	if rt.State().ScrollbackMessage != "[44%]" {
		t.Fatalf("scrollback message=%q", rt.State().ScrollbackMessage)
	}
	if !rt.ApplyAction(ScrollbackPercentAction{Visible: false, Percent: 0}).Changed {
		t.Fatalf("expected scrollback hide changed")
	}
	if rt.State().ScrollbackMessage != "" {
		t.Fatalf("expected cleared scrollback")
	}
}

func TestStatusActionFormatsMessages(t *testing.T) {
	rt := NewRuntime()
	res := rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:     StatusConnected,
		Endpoint: "https://relay.example/v1",
		Duration: 2 * time.Second,
	}})
	if !res.Changed {
		t.Fatalf("expected connected status changed")
	}
	if !strings.Contains(res.State.ConnectionMessage, "connected to https://relay.example/v1") {
		t.Fatalf("unexpected connected message: %q", res.State.ConnectionMessage)
	}

	res = rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:      StatusConnectionBackoff,
		Endpoint:  "https://relay.example/v1",
		Remaining: 1500 * time.Millisecond,
	}})
	if !res.Changed {
		t.Fatalf("expected backoff status changed")
	}
	if !strings.Contains(res.State.ConnectionMessage, "reconnecting in 2s") {
		t.Fatalf("unexpected backoff message: %q", res.State.ConnectionMessage)
	}
}

func TestAttachStatusActionClearsDisconnectThenShowsConnected(t *testing.T) {
	rt := NewRuntime()
	now := time.Now()
	rt.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(time.Second),
		Now:           now,
	}})

	res := rt.ApplyAction(AttachStatusAction{Input: AttachStatusInput{
		Connected: true,
		Kind:      StatusConnected,
		Endpoint:  "https://relay.example/v1",
		Duration:  2 * time.Second,
		Now:       now,
	}})
	if !res.Changed {
		t.Fatalf("expected attach status changed")
	}
	state := res.State
	if state.DisconnectVisible {
		t.Fatalf("disconnect overlay should be hidden")
	}
	if !strings.Contains(state.ConnectionMessage, "connected to https://relay.example/v1") {
		t.Fatalf("unexpected connection message: %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != BannerGreen {
		t.Fatalf("expected green style")
	}
}
