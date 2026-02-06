package mvu

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/theme"
)

func TestContextActionUpdatesFields(t *testing.T) {
	rt := NewRuntime()
	mock := clock.NewMock()
	changed := rt.ApplyAction(ContextAction{Input: ContextInput{
		Clock:     mock,
		Endpoint:  "https://relay.example/v1",
		SessionID: "session-a",
		Theme:     theme.TUI("default"),
	}})
	if !changed.Changed {
		t.Fatalf("expected context action to change state")
	}
	state := rt.State()
	if state.Endpoint != "https://relay.example/v1" {
		t.Fatalf("endpoint=%q", state.Endpoint)
	}
	if state.SessionID != "session-a" {
		t.Fatalf("session_id=%q", state.SessionID)
	}
}

func TestClearOverlaysActionClearsOnlyWhenActive(t *testing.T) {
	rt := NewRuntime()
	if rt.ApplyAction(ClearOverlaysAction{}).Changed {
		t.Fatalf("expected no change without overlays")
	}
	rt.ApplyAction(HelpVisibleAction{Visible: true})
	if !rt.ApplyAction(ClearOverlaysAction{}).Changed {
		t.Fatalf("expected clear overlays change")
	}
	if rt.State().HelpVisible {
		t.Fatalf("help should be hidden")
	}
}

func TestStatusAndWallActionsProvideDelayHints(t *testing.T) {
	rt := NewRuntime()
	status := rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:     StatusConnected,
		Endpoint: "https://relay.example/v1",
		Duration: 2 * time.Second,
	}})
	if !status.Changed {
		t.Fatalf("expected status change")
	}
	if status.Delay <= 0 {
		t.Fatalf("expected positive status delay")
	}

	wall := rt.ApplyAction(WallAction{Input: WallInput{
		Visible:  true,
		Title:    "Broadcast:",
		Message:  "hello",
		Duration: 2 * time.Second,
	}})
	if !wall.Changed {
		t.Fatalf("expected wall change")
	}
	if wall.Delay <= 0 {
		t.Fatalf("expected positive wall delay")
	}
	if wall.ForceFull {
		t.Fatalf("wall overlay should avoid full redraw mode")
	}
}
