package mvu

import "time"

func expireState(state *State, now time.Time) {
	if state == nil {
		return
	}
	if state.ConnectionMessage != "" && !state.ConnectionExpiresAt.IsZero() && !now.Before(state.ConnectionExpiresAt) {
		state.ConnectionMessage = ""
		state.ConnectionStyle = BannerRed
		state.ConnectionShownAt = time.Time{}
		state.ConnectionExpiresAt = time.Time{}
	}
	if state.WallVisible && !state.WallExpiresAt.IsZero() && !now.Before(state.WallExpiresAt) {
		state.WallVisible = false
		state.WallTitle = ""
		state.WallMessage = ""
		state.WallExpiresAt = time.Time{}
	}
	if state.TabBarVisible && !state.TabBarExpiresAt.IsZero() && !now.Before(state.TabBarExpiresAt) {
		state.TabBarVisible = false
		state.TabBarShownAt = time.Time{}
		state.TabBarExpiresAt = time.Time{}
	}
}

func clampActiveTab(tabCount, active int) int {
	if active < 0 {
		return 0
	}
	if tabCount == 0 {
		return 0
	}
	if active >= tabCount {
		return tabCount - 1
	}
	return active
}
