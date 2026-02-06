package session

import (
	"os"
	"strings"
	"testing"
)

func TestSessionRenderingDoesNotUseRowOnePartialPainter(t *testing.T) {
	data, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatalf("read session.go: %v", err)
	}
	src := string(data)
	forbidden := []string{
		"clearTopRow :=",
		"compositor.DrawBanner(&buf",
		"compositor.DrawTabBar(&buf",
		".PrepareRenderState(",
		"mvu.HostRenderInput{",
		"mvu.AttachRenderInput{",
		"func renderSnapshotSkipTopRow(",
		"func renderSnapshot(",
		"func maskSnapshotTopRow(",
		"func maskSnapshotRows(",
		"func helpBoxRows(",
		"func scrollbackPercent(",
		"buildScrollbackViewSnapshot(",
		"\"overlay\":             false,",
		".SetState(func(s *mvu.State)",
		"lastOverlayFull",
		"lastTabBarVisible",
		"lastTopOverlayVisible",
		"lastTopOverlayOnRow",
		"scheduleOverlayRedraw(",
		"scheduleWallHide(",
		"scheduleTabBarAutoHide(",
		"RenderFallback(",
		"func (r *Runner) useRenderer(",
		"\"connection lost to %s, reconnecting\"",
		"\"connection lost to %s, reconnecting in %ds\"",
		".ShowConnected(",
		".ShowError(",
		".ShowConnectionLost(",
		".ShowWall(",
		".ShowHelp(",
		".HideHelp(",
		".HideConnection(",
		".SetTabsFromSources(",
		".SetScrollbackMessage(",
		".ClearScrollback(",
		".ToggleTabBar(",
		".SetTabBarVisible(",
		"ui.SetClock(",
		"ui.SetEndpoint(",
		"compositor.SetClock(",
		"compositor.SetEndpoint(",
		"compositor.SetTheme(",
		"r.runtime().SetTheme(",
		"r.runtime().SetSessionID(",
		"ui.HasActiveLayers(",
		"ui.ClearAllOverlays(",
		".SyncStatus(",
		".SyncSessionTabs(",
		".SyncHelpVisible(",
		".SyncWall(",
		".SyncScrollbackPercent(",
		".SyncTabToggle(",
		".SyncTabSuppressed(",
		".SyncContext(",
		".SyncClearOverlays(",
		".SyncStatusEffect(",
		".SyncWallEffect(",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) {
			t.Fatalf("non-MVU partial render path found in session.go: %q", needle)
		}
	}
	if !strings.Contains(src, ".RenderHostFrame(") {
		t.Fatalf("expected session renderer to delegate overlay frame policy to mvu.Runtime.RenderHostFrame")
	}
	if !strings.Contains(src, "mvu.ConnectionLostMessage(") {
		t.Fatalf("expected session reconnect message formatting to delegate to mvu.ConnectionLostMessage")
	}
	if !strings.Contains(src, "mvu.ConnectionLostBackoffMessage(") {
		t.Fatalf("expected session reconnect backoff formatting to delegate to mvu.ConnectionLostBackoffMessage")
	}
	if !strings.Contains(src, "renderCache mvu.RenderCache") {
		t.Fatalf("expected session renderer state to be centralized in mvu.RenderCache")
	}
	if !strings.Contains(src, "effects     *mvu.EffectScheduler") {
		t.Fatalf("expected session timing effects to be centralized in mvu.EffectScheduler")
	}
	if !strings.Contains(src, "tabSuppress mvu.SessionTabSuppression") {
		t.Fatalf("expected session tab suppression policy to be centralized in mvu.SessionTabSuppression")
	}
	if !strings.Contains(src, ".ApplyAction(") {
		t.Fatalf("expected session ui transitions to delegate to mvu.Runtime.ApplyAction")
	}
	requiredActions := []string{
		"mvu.ContextAction{",
		"mvu.HelpVisibleAction{",
		"mvu.TabToggleAction{}",
		"mvu.ScrollbackPercentAction{",
		"mvu.ClearOverlaysAction{}",
		"mvu.TabSuppressedAction{",
		"mvu.SessionTabsAction{",
		"mvu.StatusAction{",
		"mvu.WallAction{",
	}
	for _, needle := range requiredActions {
		if !strings.Contains(src, needle) {
			t.Fatalf("expected session to use mvu action type: %q", needle)
		}
	}
}
