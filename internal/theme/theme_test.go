package theme

import (
	"fmt"
	"strings"
	"testing"
)

func TestTabMutedFgRemainsReadableAndDistinctForKeyThemes(t *testing.T) {
	t.Parallel()

	targetThemes := map[string]struct{}{
		"gruvbox-light":   {},
		"monokai-vibrant": {},
		"outrun-electric": {},
	}
	found := make(map[string]bool, len(targetThemes))
	for _, th := range All() {
		if _, ok := targetThemes[th.Name]; !ok {
			continue
		}
		found[th.Name] = true

		tabFg := mustHexColor(t, th.Name, "tab_fg", th.Tokens.TabFg)
		tabMutedFg := mustHexColor(t, th.Name, "tab_muted_fg", th.Tokens.TabMutedFg)
		tabBg := mustHexColor(t, th.Name, "tab_bg", th.Tokens.TabBg)
		tabActiveFg := mustHexColor(t, th.Name, "tab_active_fg", th.Tokens.TabActiveFg)
		tabMutedActiveFg := mustHexColor(t, th.Name, "tab_muted_active_fg", th.Tokens.TabMutedActiveFg)
		tabActiveBg := mustHexColor(t, th.Name, "tab_active_bg", th.Tokens.TabActiveBg)

		if got := contrastRatio(tabMutedFg, tabBg); got < 3.0 {
			t.Fatalf("%s: tab_muted_fg readability too low against tab_bg (contrast %.2f)", th.Name, got)
		}
		if got := colorDistance(tabMutedFg, tabFg); got < 48 {
			t.Fatalf("%s: tab_muted_fg too close to tab_fg (distance %.1f)", th.Name, got)
		}
		if tabMutedFg == (Color{R: 255, G: 255, B: 255}) {
			t.Fatalf("%s: tab_muted_fg must not be pure white", th.Name)
		}
		if got := contrastRatio(tabMutedActiveFg, tabActiveBg); got < 3.0 {
			t.Fatalf("%s: tab_muted_active_fg readability too low against tab_active_bg (contrast %.2f)", th.Name, got)
		}
		if got := colorDistance(tabMutedActiveFg, tabActiveFg); got < 48 {
			t.Fatalf("%s: tab_muted_active_fg too close to tab_active_fg (distance %.1f)", th.Name, got)
		}
		if tabMutedActiveFg == (Color{R: 255, G: 255, B: 255}) {
			t.Fatalf("%s: tab_muted_active_fg must not be pure white", th.Name)
		}
	}

	for name := range targetThemes {
		if !found[name] {
			t.Fatalf("theme %q not found", name)
		}
	}
}

func TestTabMutedFgReadableAndDistinctAcrossAllThemes(t *testing.T) {
	t.Parallel()
	for _, th := range All() {
		tabFg := mustHexColor(t, th.Name, "tab_fg", th.Tokens.TabFg)
		tabMutedFg := mustHexColor(t, th.Name, "tab_muted_fg", th.Tokens.TabMutedFg)
		tabBg := mustHexColor(t, th.Name, "tab_bg", th.Tokens.TabBg)
		tabActiveFg := mustHexColor(t, th.Name, "tab_active_fg", th.Tokens.TabActiveFg)
		tabMutedActiveFg := mustHexColor(t, th.Name, "tab_muted_active_fg", th.Tokens.TabMutedActiveFg)
		tabActiveBg := mustHexColor(t, th.Name, "tab_active_bg", th.Tokens.TabActiveBg)

		if got := contrastRatio(tabMutedFg, tabBg); got < 3.0 {
			t.Fatalf("%s: tab_muted_fg contrast %.2f < 3.0", th.Name, got)
		}
		if got := colorDistance(tabMutedFg, tabFg); got < 48 {
			t.Fatalf("%s: tab_muted_fg distance %.1f < 48", th.Name, got)
		}
		if tabMutedFg == (Color{R: 255, G: 255, B: 255}) {
			t.Fatalf("%s: tab_muted_fg must not be pure white", th.Name)
		}
		if got := contrastRatio(tabMutedActiveFg, tabActiveBg); got < 3.0 {
			t.Fatalf("%s: tab_muted_active_fg contrast %.2f < 3.0", th.Name, got)
		}
		if got := colorDistance(tabMutedActiveFg, tabActiveFg); got < 48 {
			t.Fatalf("%s: tab_muted_active_fg distance %.1f < 48", th.Name, got)
		}
		if tabMutedActiveFg == (Color{R: 255, G: 255, B: 255}) {
			t.Fatalf("%s: tab_muted_active_fg must not be pure white", th.Name)
		}
	}
}

func TestTabActiveFgFollowsTabFgForProblemThemes(t *testing.T) {
	t.Parallel()
	required := map[string]struct{}{
		"doom-dracula":    {},
		"doom-iosvkem":    {},
		"doom-nord":       {},
		"gruvbox-light":   {},
		"outrun-electric": {},
	}
	seen := map[string]bool{}
	for _, th := range All() {
		if _, ok := required[th.Name]; !ok {
			continue
		}
		seen[th.Name] = true
		if th.Tokens.TabActiveFg != th.Tokens.TabFg {
			t.Fatalf("%s: expected tab_active_fg (%s) to match tab_fg (%s)", th.Name, th.Tokens.TabActiveFg, th.Tokens.TabFg)
		}
	}
	for name := range required {
		if !seen[name] {
			t.Fatalf("theme %q not found", name)
		}
	}
}

func mustHexColor(t *testing.T, themeName, tokenName, value string) Color {
	t.Helper()
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 7 || !strings.HasPrefix(trimmed, "#") {
		t.Fatalf("%s: %s has invalid hex format: %q", themeName, tokenName, value)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(trimmed, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("%s: %s parse failed for %q: %v", themeName, tokenName, value, err)
	}
	return Color{R: r, G: g, B: b}
}
