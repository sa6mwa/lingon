package mvu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostAttachUseActionAPIOnly(t *testing.T) {
	targets := []string{
		filepath.Join("..", "attach", "client.go"),
		filepath.Join("..", "attach", "multi.go"),
		filepath.Join("..", "session", "session.go"),
		filepath.Join("..", "session", "remote.go"),
	}
	for _, target := range targets {
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read %s: %v", target, err)
		}
		src := string(data)
		if !strings.Contains(src, ".ApplyAction(") {
			t.Fatalf("expected action API usage in %s", target)
		}
		forbidden := []string{
			".Dispatch(",
			".SyncContext(",
			".SyncStatus(",
			".SyncStatusEffect(",
			".SyncAttachStatus(",
			".SyncAttachStatusEffect(",
			".SyncAttachConnectivity(",
			".SyncSessionTabs(",
			".SyncHelpVisible(",
			".SyncTabWake(",
			".SyncTabToggle(",
			".SyncTabSuppressed(",
			".SyncScrollbackPercent(",
			".SyncWall(",
			".SyncWallEffect(",
			".SyncClearOverlays(",
			".ShowHelp(",
			".HideHelp(",
			".ShowConnected(",
			".ShowError(",
			".ShowConnectionLost(",
			".ShowWall(",
			".SetTabsFromSources(",
			".WakeTabs(",
			".ToggleTabBar(",
			".SetTabBarVisible(",
		}
		for _, needle := range forbidden {
			if strings.Contains(src, needle) {
				t.Fatalf("found forbidden non-action runtime call in %s: %q", target, needle)
			}
		}
	}
}
