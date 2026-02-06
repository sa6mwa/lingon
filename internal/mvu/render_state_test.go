package mvu

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/theme"
)

func TestRuntimePrepareRenderStateSuppressTabs(t *testing.T) {
	rt := NewRuntime()
	now := time.Now()
	state := State{
		Theme:         theme.TUI("default"),
		TabBarVisible: true,
		Tabs:          []Tab{{Index: 1, Title: "tab-a"}},
	}
	renderState, resolve, delay := rt.PrepareRenderState(state, Cursor{Row: 2, Col: 1, Visible: true}, now, RenderStateOptions{
		SuppressTabs: true,
	})
	if renderState.TabBarVisible {
		t.Fatalf("expected render state tabs hidden when suppressed")
	}
	if !resolve.SuppressTabs {
		t.Fatalf("expected suppress-tabs resolve option")
	}
	if resolve.ForceTabsVisible {
		t.Fatalf("expected force-tabs resolve option disabled")
	}
	if delay != 0 {
		t.Fatalf("expected no tab delay when suppressed, got %v", delay)
	}
}

func TestRuntimePrepareRenderStateForceTabs(t *testing.T) {
	rt := NewRuntime()
	now := time.Now()
	state := State{
		Theme:         theme.TUI("default"),
		TabBarVisible: true,
		Tabs:          []Tab{{Index: 1, Title: "tab-a"}},
	}
	renderState, resolve, delay := rt.PrepareRenderState(state, Cursor{Row: 1, Col: 1, Visible: true}, now, RenderStateOptions{
		ForceTabsVisible: true,
	})
	if !renderState.TabBarVisible {
		t.Fatalf("expected render state tabs forced visible")
	}
	if !resolve.ForceTabsVisible {
		t.Fatalf("expected force-tabs resolve option")
	}
	if resolve.SuppressTabs {
		t.Fatalf("expected suppress-tabs resolve option disabled")
	}
	if delay != 0 {
		t.Fatalf("expected no tab delay when forced, got %v", delay)
	}
}
