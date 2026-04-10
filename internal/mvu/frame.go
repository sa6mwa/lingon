package mvu

import (
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"strings"
	"time"
)

// ResolveOptions controls MVU state resolution.
type ResolveOptions struct {
	SuppressTabs bool
	// HideTabsOnTopRow forces tabs hidden when cursor is on row 1.
	HideTabsOnTopRow bool
	// ForceTabsVisible keeps tabs visible regardless of top-row cursor auto-hide.
	ForceTabsVisible bool
}

// Resolved represents one fully reduced MVU model state ready for rendering.
type Resolved struct {
	State              State
	TabBarVisible      bool
	ConnectionVisible  bool
	LoadingVisible     bool
	ScrollbackVisible  bool
	DisconnectVisible  bool
	HelpVisible        bool
	WallVisible        bool
	TopOverlayVisible  bool
	FullOverlayVisible bool
}

// Resolve applies MVU update rules to raw UI state and cursor context.
func Resolve(state State, cursor Cursor, now time.Time, opts ResolveOptions) Resolved {
	resolved := state
	if resolved.ConnectionMessage != "" && !resolved.ConnectionExpiresAt.IsZero() && !now.Before(resolved.ConnectionExpiresAt) {
		resolved.ConnectionMessage = ""
		resolved.ConnectionShownAt = time.Time{}
		resolved.ConnectionExpiresAt = time.Time{}
	}
	loadingVisible := resolved.LoadingMessage != ""
	if resolved.WallVisible && !resolved.WallExpiresAt.IsZero() && !now.Before(resolved.WallExpiresAt) {
		resolved.WallVisible = false
		resolved.WallTitle = ""
		resolved.WallMessage = ""
		resolved.WallExpiresAt = time.Time{}
	}
	if resolved.TabBarVisible && !resolved.TabBarExpiresAt.IsZero() && !now.Before(resolved.TabBarExpiresAt) {
		resolved.TabBarVisible = false
		resolved.TabBarShownAt = time.Time{}
		resolved.TabBarExpiresAt = time.Time{}
	}
	connectionVisible := resolved.ConnectionMessage != ""
	scrollbackVisible := resolved.ScrollbackMessage != ""
	disconnectVisible := resolved.DisconnectVisible && resolved.DisconnectTitle != ""
	if disconnectVisible {
		loadingVisible = false
	}
	if connectionVisible {
		loadingVisible = false
	}
	tabBarVisible := resolved.TabBarVisible && len(resolved.Tabs) > 0
	switch {
	case scrollbackVisible:
		tabBarVisible = false
		loadingVisible = false
	case opts.SuppressTabs:
		tabBarVisible = false
	case opts.ForceTabsVisible:
		tabBarVisible = resolved.TabBarVisible && len(resolved.Tabs) > 0
	case cursor.Row <= 1:
		tabBarVisible = false
	case opts.HideTabsOnTopRow && cursor.Row <= 1:
		tabBarVisible = false
	}
	// DO NOT REMOVE: hard requirement.
	// Scrollback indicator owns the top-row status channel while active.
	// Connection/tab overlays must not compete with scrollback rendering.
	if scrollbackVisible {
		connectionVisible = false
	}
	helpVisible := resolved.HelpVisible
	wallVisible := resolved.WallVisible && resolved.WallTitle != ""
	resolved.TabBarVisible = tabBarVisible
	if !connectionVisible {
		resolved.ConnectionMessage = ""
		resolved.ConnectionShownAt = time.Time{}
		resolved.ConnectionExpiresAt = time.Time{}
	}
	if !loadingVisible {
		resolved.LoadingMessage = ""
	}
	if !scrollbackVisible {
		resolved.ScrollbackMessage = ""
	}
	topOverlayVisible := tabBarVisible || connectionVisible || loadingVisible || scrollbackVisible
	fullOverlayVisible := disconnectVisible || helpVisible || wallVisible
	return Resolved{
		State:              resolved,
		TabBarVisible:      tabBarVisible,
		ConnectionVisible:  connectionVisible,
		LoadingVisible:     loadingVisible,
		ScrollbackVisible:  scrollbackVisible,
		DisconnectVisible:  disconnectVisible,
		HelpVisible:        helpVisible,
		WallVisible:        wallVisible,
		TopOverlayVisible:  topOverlayVisible,
		FullOverlayVisible: fullOverlayVisible,
	}
}

// ComposeResolved renders a fully reduced MVU model.
func ComposeResolved(base []byte, cols, rows int, cursor Cursor, resolved Resolved) []byte {
	state := resolved.State
	var buf bytes.Buffer
	if len(base) > 0 {
		buf.Grow(len(base) + cols*rows)
		buf.Write(base)
	}
	if resolved.DisconnectVisible {
		lines := []string{state.DisconnectTitle}
		if state.DisconnectDetail != "" {
			lines = append(lines, "", state.DisconnectDetail)
		}
		DrawHelpBoxWithMinWidth(&buf, cols, rows, state.Theme, lines, state.DisconnectBoxWidth)
	}
	if resolved.WallVisible {
		DrawWallBox(&buf, cols, rows, state.Theme, state.WallTitle, state.WallMessage)
	}
	if resolved.HelpVisible {
		DrawHelpBox(&buf, cols, rows, state.Theme, HelpLines(state))
	}
	composeTopOverlay(&buf, cols, cursor, resolved, true, 0, 0)
	return buf.Bytes()
}

// ComposeTopOverlayResolved renders only top-row overlays (tabs/banners) and cursor.
func ComposeTopOverlayResolved(cols int, cursor Cursor, resolved Resolved) []byte {
	return composeTopOverlayResolved(cols, cursor, resolved, true, 0, 0)
}

// ComposeTopOverlayResolvedNoTabs renders top-row overlays without repainting tabs.
func ComposeTopOverlayResolvedNoTabs(cols int, cursor Cursor, resolved Resolved) []byte {
	return composeTopOverlayResolved(cols, cursor, resolved, false, 0, 0)
}

// ComposeTopOverlayResolvedNoTabsPadded renders top-row overlays without tab
// repaint and pads status text to overwrite prior badge widths.
func ComposeTopOverlayResolvedNoTabsPadded(cols int, cursor Cursor, resolved Resolved, prevConnectionLen, prevScrollbackLen int) []byte {
	return composeTopOverlayResolved(cols, cursor, resolved, false, prevConnectionLen, prevScrollbackLen)
}

func composeTopOverlayResolved(cols int, cursor Cursor, resolved Resolved, includeTabs bool, prevConnectionLen, prevScrollbackLen int) []byte {
	var buf bytes.Buffer
	composeTopOverlay(&buf, cols, cursor, resolved, includeTabs, prevConnectionLen, prevScrollbackLen)
	return buf.Bytes()
}

func composeTopOverlay(buf *bytes.Buffer, cols int, cursor Cursor, resolved Resolved, includeTabs bool, prevConnectionLen, prevScrollbackLen int) {
	state := resolved.State
	if includeTabs && resolved.TabBarVisible {
		DrawTabBar(buf, cols, state.Tabs, state.ActiveTab, state.Theme)
	}
	if resolved.ConnectionVisible {
		msg := state.ConnectionMessage
		if prevConnectionLen > len(msg) {
			if !includeTabs && resolved.TabBarVisible {
				DrawTabBasePadAtRow(buf, cols, 1, prevConnectionLen, len(msg), state.Theme)
			} else {
				msg = strings.Repeat(" ", prevConnectionLen-len(msg)) + msg
			}
		}
		DrawBanner(buf, cols, msg, state.ConnectionStyle, state.Theme)
	}
	if resolved.LoadingVisible {
		DrawBanner(buf, cols, state.LoadingMessage, BannerYellow, state.Theme)
	}
	if resolved.ScrollbackVisible {
		msg := state.ScrollbackMessage
		if prevScrollbackLen > len(msg) {
			if !includeTabs && resolved.TabBarVisible {
				DrawTabBasePadAtRow(buf, cols, 1, prevScrollbackLen, len(msg), state.Theme)
			} else {
				msg = strings.Repeat(" ", prevScrollbackLen-len(msg)) + msg
			}
		}
		DrawIndicator(buf, cols, msg, BannerGreen, state.Theme)
	}
	WriteCursor(buf, cursor)
}

func tabBarSignature(cols int, state State) uint64 {
	if cols <= 0 || len(state.Tabs) == 0 {
		return 0
	}
	h := fnv.New64a()
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(cols))
	_, _ = h.Write(n[:])
	binary.LittleEndian.PutUint64(n[:], uint64(state.ActiveTab))
	_, _ = h.Write(n[:])
	writeSigString := func(v string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(v)))
		_, _ = h.Write(n[:])
		if v != "" {
			_, _ = h.Write([]byte(v))
		}
	}
	writeSigBool := func(v bool) {
		if v {
			_, _ = h.Write([]byte{1})
			return
		}
		_, _ = h.Write([]byte{0})
	}
	writeSigString(state.Theme.TabBg)
	writeSigString(state.Theme.TabFg)
	writeSigString(state.Theme.TabMutedFg)
	writeSigString(state.Theme.TabMutedActiveFg)
	writeSigString(state.Theme.TabActiveBg)
	writeSigString(state.Theme.TabActiveFg)
	writeSigString(state.Theme.Reset)
	for _, tab := range state.Tabs {
		binary.LittleEndian.PutUint64(n[:], uint64(tab.Index))
		_, _ = h.Write(n[:])
		writeSigString(tab.Title)
		writeSigBool(tab.Disabled)
		writeSigBool(tab.Muted)
	}
	return h.Sum64()
}
