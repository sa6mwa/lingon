package attach

import (
	"os"
	"strings"
	"testing"
)

func TestAttachRenderingDoesNotUseOverlayOnlyComposePaths(t *testing.T) {
	clientSrcBytes, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatalf("read client.go: %v", err)
	}
	clientSrc := string(clientSrcBytes)
	forbiddenClient := []string{
		"ComposeWithState(nil",
		"compositor.TabBarOverlay(",
		"func disconnectLines(",
		"func helpLines(",
		"func helpBoxRows(",
		"func maskSnapshotRows(",
		"func buildScrollbackViewSnapshot(",
		"func scrollbackPercent(",
		".PrepareRenderState(",
		"mvu.AttachRenderInput{",
		"mvu.HostRenderInput{",
		"RenderSnapshotViewportNoClear(c.stdoutWriter(), viewSnap",
		"render.SnapshotViewportDelta(c.stdoutWriter(), prev, viewSnap",
		".SetState(func(s *mvu.State)",
		"lastRenderCols",
		"lastRenderRows",
		"lastSnapCols",
		"lastSnapRows",
		"lastRenderSnap",
		"lastTopOverlay",
		"lastDisconnect",
		"lastHelp",
		"lastWall",
		"lastScrollback",
		"scheduleOverlayAutoHide(",
		"scheduleWallAutoHide(",
		"scheduleTabBarAutoHide(",
		"RequestRenderFallback(",
		"func (c *Client) RenderTabBarOverlay(",
		"func (c *Client) RenderOverlay(",
		"compositor.Compose(",
		".ShowConnected(",
		".ShowError(",
		".ShowWall(",
		".ShowHelp(",
		".HideHelp(",
		".SetScrollbackMessage(",
		".ClearScrollback(",
		".ToggleTabBar(",
		".SetClock(",
		".SetSessionID(",
		".SetEndpoint(",
		".SetTheme(",
		".SyncStatus(",
		".SyncWall(",
		".SyncHelpVisible(",
		".SyncScrollbackPercent(",
		".SyncTabToggle(",
		".SyncContext(",
		".SyncStatusEffect(",
		".SyncWallEffect(",
	}
	for _, needle := range forbiddenClient {
		if strings.Contains(clientSrc, needle) {
			t.Fatalf("non-MVU overlay-only path found in attach client: %q", needle)
		}
	}
	if !strings.Contains(clientSrc, ".RenderAttachFrame(") {
		t.Fatalf("expected attach renderer to delegate overlay frame policy to mvu.Runtime.RenderAttachFrame")
	}
	if !strings.Contains(clientSrc, ".RenderDisabledFrame(") {
		t.Fatalf("expected attach disabled renderer to delegate to mvu.Runtime.RenderDisabledFrame")
	}
	if !strings.Contains(clientSrc, "renderCache      mvu.RenderCache") {
		t.Fatalf("expected attach renderer state to be centralized in mvu.RenderCache")
	}
	if !strings.Contains(clientSrc, "scrollbackBuffer *mvu.ProtoScrollbackBuffer") {
		t.Fatalf("expected attach scrollback storage to be centralized in mvu.ProtoScrollbackBuffer")
	}
	if !strings.Contains(clientSrc, "scrollbackView   mvu.ScrollbackViewport") {
		t.Fatalf("expected attach scrollback viewport to be centralized in mvu.ScrollbackViewport")
	}
	if !strings.Contains(clientSrc, "effects          *mvu.EffectScheduler") {
		t.Fatalf("expected attach timing effects to be centralized in mvu.EffectScheduler")
	}
	if !strings.Contains(clientSrc, "tabSuppress      mvu.CursorTabSuppression") {
		t.Fatalf("expected attach tab suppression policy to be centralized in mvu.CursorTabSuppression")
	}
	if !strings.Contains(clientSrc, ".ApplyAction(") {
		t.Fatalf("expected attach client ui transitions to delegate to mvu.Runtime.ApplyAction")
	}
	requiredClientActions := []string{
		"mvu.ContextAction{",
		"mvu.ScrollbackPercentAction{",
		"mvu.StatusAction{",
		"mvu.WallAction{",
		"mvu.HelpVisibleAction{",
		"mvu.TabToggleAction{}",
	}
	for _, needle := range requiredClientActions {
		if !strings.Contains(clientSrc, needle) {
			t.Fatalf("expected attach client to use mvu action type: %q", needle)
		}
	}

	multiSrcBytes, err := os.ReadFile("multi.go")
	if err != nil {
		t.Fatalf("read multi.go: %v", err)
	}
	multiSrc := string(multiSrcBytes)
	forbiddenMulti := []string{
		".RenderOverlay()",
		".RenderTabBarOverlay()",
		".SetState(func(s *mvu.State)",
		".ShowDisconnected(",
		".HideDisconnected(",
		".HideConnection(",
		".ShowConnected(",
		".ShowError(",
		".ShowHelp(",
		".HideHelp(",
		".SetTabsFromSources(",
		".WakeTabs(",
		".ToggleTabBar(",
		".SetTheme(",
		".SyncAttachConnectivity(",
		".SyncAttachStatusEffect(",
		".SyncSessionTabs(",
		".SyncHelpVisible(",
		".SyncTabWake(",
		".SyncTabToggle(",
		".SyncContext(",
		".RequestRenderFallback(",
	}
	for _, needle := range forbiddenMulti {
		if strings.Contains(multiSrc, needle) {
			t.Fatalf("non-MVU overlay-only presenter call found in attach multi: %q", needle)
		}
	}
	if !strings.Contains(multiSrc, ".ApplyAction(") {
		t.Fatalf("expected attach multi ui transitions to delegate to mvu.Runtime.ApplyAction")
	}
	requiredMultiActions := []string{
		"mvu.ContextAction{",
		"mvu.AttachConnectivityAction{",
		"mvu.AttachStatusAction{",
		"mvu.SessionTabsAction{",
		"mvu.HelpVisibleAction{",
		"mvu.TabWakeAction{",
		"mvu.TabToggleAction{}",
	}
	for _, needle := range requiredMultiActions {
		if !strings.Contains(multiSrc, needle) {
			t.Fatalf("expected attach multi to use mvu action type: %q", needle)
		}
	}
	if !strings.Contains(multiSrc, "mvu.NewAttachWaitController(") {
		t.Fatalf("expected attach multi wait-for-sessions policy to delegate to mvu.NewAttachWaitController")
	}
	if !strings.Contains(multiSrc, "mvu.ContextAction{") {
		t.Fatalf("expected attach multi context/bootstrap to delegate to mvu.ContextAction via ApplyAction")
	}
	if !strings.Contains(multiSrc, "mvu.NewEffectScheduler(") {
		t.Fatalf("expected attach multi timer effects to use mvu.NewEffectScheduler")
	}
	if !strings.Contains(multiSrc, "mvu.ConnectedToMessage(") {
		t.Fatalf("expected attach multi connection status formatting to delegate to mvu.ConnectedToMessage")
	}
}
