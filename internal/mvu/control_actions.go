package mvu

import "pkt.systems/lingon/internal/control"

// ActionForControl maps control-layer UI actions to MVU actions.
func ActionForControl(action control.Action) (Action, bool) {
	switch action {
	case control.ActionHelp:
		return HelpVisibleAction{Visible: true}, true
	case control.ActionToggleTabBar:
		return TabToggleAction{}, true
	default:
		return nil, false
	}
}

// ActionForHelpDismissKey maps a key press to the "close help overlay" action.
func ActionForHelpDismissKey(helpVisible bool, b byte) (Action, bool) {
	if !helpVisible {
		return nil, false
	}
	if b == 'q' || b == 'Q' {
		return HelpVisibleAction{Visible: false}, true
	}
	return nil, false
}
