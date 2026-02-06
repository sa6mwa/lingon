package mvu

import (
	"testing"

	"pkt.systems/lingon/internal/control"
)

func TestActionForControlMapping(t *testing.T) {
	action, ok := ActionForControl(control.ActionHelp)
	if !ok || action == nil {
		t.Fatalf("expected help control action mapping")
	}
	help, ok := action.(HelpVisibleAction)
	if !ok {
		t.Fatalf("expected HelpVisibleAction type, got %T", action)
	}
	if !help.Visible {
		t.Fatalf("expected help action to set Visible=true")
	}

	action, ok = ActionForControl(control.ActionToggleTabBar)
	if !ok || action == nil {
		t.Fatalf("expected toggle-tabbar control action mapping")
	}
	if _, ok := action.(TabToggleAction); !ok {
		t.Fatalf("expected TabToggleAction type, got %T", action)
	}

	if action, ok := ActionForControl(control.ActionNextTab); ok || action != nil {
		t.Fatalf("expected non-ui control action to not map")
	}
}

func TestActionForHelpDismissKey(t *testing.T) {
	if action, ok := ActionForHelpDismissKey(false, 'q'); ok || action != nil {
		t.Fatalf("expected no dismiss action when help hidden")
	}
	for _, b := range []byte{'q', 'Q'} {
		action, ok := ActionForHelpDismissKey(true, b)
		if !ok || action == nil {
			t.Fatalf("expected dismiss action for key %q", b)
		}
		help, ok := action.(HelpVisibleAction)
		if !ok {
			t.Fatalf("expected HelpVisibleAction type for key %q, got %T", b, action)
		}
		if help.Visible {
			t.Fatalf("expected dismiss key to set Visible=false")
		}
	}
	for _, b := range []byte{0x1b, '\r', '\n'} {
		if action, ok := ActionForHelpDismissKey(true, b); ok || action != nil {
			t.Fatalf("expected non-dismiss key %q to not dismiss help", b)
		}
	}
	if action, ok := ActionForHelpDismissKey(true, 'x'); ok || action != nil {
		t.Fatalf("expected non-dismiss key to return nil action")
	}
}
