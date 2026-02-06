package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/theme"
)

func TestModelResolveAndCompose(t *testing.T) {
	now := time.Now()
	model := NewModel(State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "tab-a"}},
		ConnectionMessage: "connected",
		ConnectionStyle:   BannerGreen,
	}, Cursor{Row: 2, Col: 4, Visible: true}, now, ResolveOptions{})

	resolved := model.ResolveState()
	if !resolved.TopOverlayVisible {
		t.Fatalf("expected top overlay visible")
	}
	out := model.Compose([]byte("\x1b[2;1Hpayload"), 80, 10)
	raw := string(out)
	if !strings.Contains(raw, "tab-a") {
		t.Fatalf("expected tab content to remain in model compose under banner overlay")
	}
	if !strings.Contains(raw, "connected") {
		t.Fatalf("expected banner content in model compose")
	}
}

func TestModelComposeTopOverlay(t *testing.T) {
	now := time.Now()
	model := NewModel(State{
		Theme:         theme.TUI("default"),
		TabBarVisible: true,
		Tabs:          []Tab{{Index: 1, Title: "tab-a"}},
	}, Cursor{Row: 2, Col: 1, Visible: true}, now, ResolveOptions{})
	out := model.ComposeTopOverlay(80)
	if len(out) == 0 {
		t.Fatalf("expected top overlay bytes")
	}
	if !strings.Contains(string(out), "tab-a") {
		t.Fatalf("expected top overlay tab label")
	}
}

func TestModelWithMutators(t *testing.T) {
	now := time.Now()
	base := NewModel(State{
		Theme: theme.TUI("default"),
	}, Cursor{Row: 1, Col: 1, Visible: true}, now, ResolveOptions{})

	updated := base.
		WithState(State{Theme: theme.TUI("default"), TabBarVisible: true, Tabs: []Tab{{Index: 1, Title: "tab"}}}).
		WithCursor(Cursor{Row: 2, Col: 3, Visible: true}).
		WithNow(now.Add(time.Second)).
		WithResolve(ResolveOptions{ForceTabsVisible: true})

	if updated.Cursor.Row != 2 || updated.Cursor.Col != 3 {
		t.Fatalf("expected cursor mutator to apply")
	}
	if !updated.Now.After(base.Now) {
		t.Fatalf("expected timestamp mutator to apply")
	}
	if !updated.Resolve.ForceTabsVisible {
		t.Fatalf("expected resolve mutator to apply")
	}
	if len(updated.State.Tabs) != 1 {
		t.Fatalf("expected state mutator to apply")
	}
}
