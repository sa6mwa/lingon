package mvu

import (
	"testing"
	"time"
)

func TestExpireStateExpiresConnectionWallAndTabs(t *testing.T) {
	now := time.Now()
	state := State{
		ConnectionMessage:   "connected",
		ConnectionStyle:     BannerGreen,
		ConnectionShownAt:   now.Add(-2 * time.Second),
		ConnectionExpiresAt: now.Add(-time.Second),
		WallVisible:         true,
		WallTitle:           "Broadcast:",
		WallMessage:         "hello",
		WallExpiresAt:       now.Add(-time.Second),
		TabBarVisible:       true,
		TabBarShownAt:       now.Add(-2 * time.Second),
		TabBarExpiresAt:     now.Add(-time.Second),
	}

	expireState(&state, now)
	if state.ConnectionMessage != "" || !state.ConnectionShownAt.IsZero() || !state.ConnectionExpiresAt.IsZero() {
		t.Fatalf("expected connection banner expired, got %+v", state)
	}
	if state.ConnectionStyle != BannerRed {
		t.Fatalf("expected expired connection style reset to red")
	}
	if state.WallVisible || state.WallTitle != "" || state.WallMessage != "" || !state.WallExpiresAt.IsZero() {
		t.Fatalf("expected wall expired, got %+v", state)
	}
	if state.TabBarVisible || !state.TabBarShownAt.IsZero() || !state.TabBarExpiresAt.IsZero() {
		t.Fatalf("expected tab wake expired, got %+v", state)
	}
}

func TestClampActiveTab(t *testing.T) {
	cases := []struct {
		name   string
		tabs   int
		active int
		want   int
	}{
		{name: "negative", tabs: 3, active: -1, want: 0},
		{name: "empty", tabs: 0, active: 4, want: 0},
		{name: "within", tabs: 3, active: 1, want: 1},
		{name: "overflow", tabs: 3, active: 9, want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampActiveTab(tc.tabs, tc.active); got != tc.want {
				t.Fatalf("clampActiveTab(%d,%d)=%d want %d", tc.tabs, tc.active, got, tc.want)
			}
		})
	}
}
