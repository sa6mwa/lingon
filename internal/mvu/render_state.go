package mvu

import "time"

// RenderStateOptions controls MVU-owned tab visibility policy for a frame.
type RenderStateOptions struct {
	SuppressTabs     bool
	ForceTabsVisible bool
}

// PrepareRenderState applies MVU tab visibility policy and returns the state and
// resolve options to use for a render frame, along with the next tab auto-hide delay.
func (r *Runtime) PrepareRenderState(state State, cursor Cursor, now time.Time, opts RenderStateOptions) (State, ResolveOptions, time.Duration) {
	tabVisible, tabDelay := r.TabBarVisibility(state, cursor, now)
	if opts.SuppressTabs {
		tabVisible = false
		tabDelay = 0
	}
	if opts.ForceTabsVisible {
		tabVisible = state.TabBarVisible && len(state.Tabs) > 0
		tabDelay = 0
	}
	renderState := state
	renderState.TabBarVisible = tabVisible
	resolveOpts := ResolveOptions{}
	if opts.SuppressTabs && !opts.ForceTabsVisible {
		resolveOpts.SuppressTabs = true
	}
	if opts.ForceTabsVisible {
		resolveOpts.ForceTabsVisible = true
	}
	return renderState, resolveOpts, tabDelay
}
