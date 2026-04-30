package attach

import (
	"os"
	"regexp"
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
	if !regexp.MustCompile(`renderCache\s+mvu\.RenderCache`).MatchString(clientSrc) {
		t.Fatalf("expected attach renderer state to be centralized in mvu.RenderCache")
	}
	if !regexp.MustCompile(`scrollbackBuffer\s+\*mvu\.ProtoScrollbackBuffer`).MatchString(clientSrc) {
		t.Fatalf("expected attach scrollback storage to be centralized in mvu.ProtoScrollbackBuffer")
	}
	if !regexp.MustCompile(`scrollbackView\s+mvu\.ScrollbackViewport`).MatchString(clientSrc) {
		t.Fatalf("expected attach scrollback viewport to be centralized in mvu.ScrollbackViewport")
	}
	if !regexp.MustCompile(`effects\s+\*mvu\.EffectScheduler`).MatchString(clientSrc) {
		t.Fatalf("expected attach timing effects to be centralized in mvu.EffectScheduler")
	}
	if !regexp.MustCompile(`tabSuppress\s+mvu\.CursorTabSuppression`).MatchString(clientSrc) {
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

func TestAttachRenderCacheReadsUseSerializedHelpers(t *testing.T) {
	clientSrcBytes, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatalf("read client.go: %v", err)
	}
	clientSrc := string(clientSrcBytes)

	requiredHelpers := []string{
		"func (c *Client) renderSnapshotRows() int",
		"func (c *Client) renderHelpVisible() bool",
		"func (c *Client) applyCompositorAction(action mvu.Action) mvu.ActionResult",
		"func (c *Client) readCompositorState() mvu.State",
	}
	for _, needle := range requiredHelpers {
		if !strings.Contains(clientSrc, needle) {
			t.Fatalf("missing serialized attach render-state helper %q", needle)
		}
	}

	if got := strings.Count(clientSrc, "c.renderCache.SnapshotRows()"); got != 1 {
		t.Fatalf("expected renderCache SnapshotRows reads to be isolated in renderSnapshotRows helper, got %d direct reads", got)
	}
	if got := strings.Count(clientSrc, "c.renderCache.HelpVisible()"); got != 1 {
		t.Fatalf("expected renderCache HelpVisible reads to be isolated in renderHelpVisible helper, got %d direct reads", got)
	}
	if !regexp.MustCompile(`func \(c \*Client\) renderSnapshotRows\(\) int \{\s*c\.renderMu\.Lock\(\)`).MatchString(clientSrc) {
		t.Fatalf("expected renderSnapshotRows to serialize on renderMu")
	}
	if !regexp.MustCompile(`func \(c \*Client\) renderHelpVisible\(\) bool \{\s*c\.renderMu\.Lock\(\)`).MatchString(clientSrc) {
		t.Fatalf("expected renderHelpVisible to serialize on renderMu")
	}
}
