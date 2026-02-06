package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/theme"
)

func TestApplyActionContextAndTabs(t *testing.T) {
	rt := NewRuntime()
	ctxRes := rt.ApplyAction(ContextAction{Input: ContextInput{
		Endpoint:  "https://relay.example/v1",
		SessionID: "session-a",
		Theme:     theme.TUI("default"),
	}})
	if !ctxRes.Changed {
		t.Fatalf("expected context action to change state")
	}
	tabRes := rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Sources:  []SessionTabSource{{ID: "session-a", Name: "alpha"}},
		ActiveID: "session-a",
	}})
	if !tabRes.Changed {
		t.Fatalf("expected tabs action to change state")
	}
	if len(tabRes.Tabs) != 1 || tabRes.Active != 0 {
		t.Fatalf("unexpected tab result: len=%d active=%d", len(tabRes.Tabs), tabRes.Active)
	}
}

func TestApplyActionOverlayAndStatus(t *testing.T) {
	rt := NewRuntime()
	status := rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:     StatusConnected,
		Endpoint: "https://relay.example/v1",
		Duration: 2 * time.Second,
	}})
	if !status.Changed {
		t.Fatalf("expected status changed")
	}
	if status.Delay <= 0 {
		t.Fatalf("expected status delay")
	}
	if !strings.Contains(status.State.ConnectionMessage, "connected") {
		t.Fatalf("unexpected status message: %q", status.State.ConnectionMessage)
	}

	wall := rt.ApplyAction(WallAction{Input: WallInput{
		Visible:  true,
		Title:    "Broadcast:",
		Message:  "hello",
		Duration: 2 * time.Second,
	}})
	if !wall.Changed {
		t.Fatalf("expected wall changed")
	}
	if wall.ForceFull {
		t.Fatalf("expected wall action to avoid full redraw mode")
	}

	clear := rt.ApplyAction(ClearOverlaysAction{})
	if !clear.Changed {
		t.Fatalf("expected clear overlays changed")
	}
	if clear.State.WallVisible || clear.State.ConnectionMessage != "" {
		t.Fatalf("expected overlays cleared")
	}
}

func TestApplyActionHelpScrollbackAndAttachConnectivity(t *testing.T) {
	rt := NewRuntime()
	help := rt.ApplyAction(HelpVisibleAction{Visible: true})
	if !help.Changed || !help.State.HelpVisible {
		t.Fatalf("expected help visible")
	}
	scroll := rt.ApplyAction(ScrollbackPercentAction{Visible: true, Percent: 77})
	if !scroll.Changed || scroll.State.ScrollbackMessage != "[77%]" {
		t.Fatalf("expected scrollback message set")
	}
	attach := rt.ApplyAction(AttachConnectivityAction{Input: AttachConnectivityInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   time.Now().Add(2 * time.Second),
		Endpoint:      "https://relay.example/v1",
	}})
	if !attach.Changed || !attach.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	overlay := rt.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected: true,
		Now:       time.Now(),
	}})
	if !overlay.Changed {
		t.Fatalf("expected disconnected overlay action changed")
	}
}

func TestApplyActionTabOps(t *testing.T) {
	rt := NewRuntime()
	rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Sources:  []SessionTabSource{{ID: "a", Name: "alpha"}},
		ActiveID: "a",
	}})
	if !rt.ApplyAction(TabWakeAction{Duration: time.Second}).Changed {
		t.Fatalf("expected tab wake changed")
	}
	toggle := rt.ApplyAction(TabToggleAction{})
	if toggle.Visible {
		t.Fatalf("expected toggle to hide")
	}
	if rt.ApplyAction(TabSuppressedAction{Suppressed: false}).Changed {
		t.Fatalf("expected unsuppress to be a no-op when not suppressed")
	}
}

func TestApplyActionTabSuppressionRestoresPriorVisibility(t *testing.T) {
	rt := NewRuntime()
	rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Sources:  []SessionTabSource{{ID: "a", Name: "alpha"}},
		ActiveID: "a",
	}})

	// User explicitly hides tabs.
	rt.ApplyAction(TabToggleAction{})
	if rt.State().TabBarVisible {
		t.Fatalf("expected tabs hidden after explicit toggle")
	}
	if rt.ApplyAction(TabSuppressedAction{Suppressed: true}).Changed {
		t.Fatalf("expected suppress to be a no-op when tabs already hidden")
	}
	if rt.ApplyAction(TabSuppressedAction{Suppressed: false}).Changed {
		t.Fatalf("expected unsuppress to preserve explicit hidden state")
	}
	if rt.State().TabBarVisible {
		t.Fatalf("expected tabs to remain hidden after unsuppress")
	}

	// User explicitly shows tabs.
	if !rt.ApplyAction(TabToggleAction{}).Visible {
		t.Fatalf("expected toggle to show tabs")
	}
	if !rt.ApplyAction(TabSuppressedAction{Suppressed: true}).Changed {
		t.Fatalf("expected suppress to hide visible tabs")
	}
	if rt.State().TabBarVisible {
		t.Fatalf("expected tabs hidden while suppressed")
	}
	if !rt.ApplyAction(TabSuppressedAction{Suppressed: false}).Changed {
		t.Fatalf("expected unsuppress to restore visible tabs")
	}
	if !rt.State().TabBarVisible {
		t.Fatalf("expected tabs visible after unsuppress restore")
	}
}
