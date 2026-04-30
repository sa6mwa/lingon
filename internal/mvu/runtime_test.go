package mvu

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/theme"
)

func TestResolveHideTabsOnTopRowWithBanner(t *testing.T) {
	now := time.Now()
	state := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "tab-a"}},
		ActiveTab:         0,
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
		ConnectionShownAt: now,
	}
	resolved := Resolve(state, Cursor{Row: 1, Col: 8, Visible: true}, now, ResolveOptions{
		HideTabsOnTopRow: true,
	})
	if resolved.TabBarVisible {
		t.Fatalf("expected tab bar hidden when top-row ownership is enabled")
	}
	if !resolved.ConnectionVisible {
		t.Fatalf("expected connection banner to remain visible")
	}
}

func TestResolveHideTabsWhileScrollbackVisible(t *testing.T) {
	now := time.Now()
	state := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "tab-a"}},
		ActiveTab:         0,
		ScrollbackMessage: "[100%]",
	}
	resolved := Resolve(state, Cursor{Row: 10, Col: 1, Visible: true}, now, ResolveOptions{})
	if resolved.TabBarVisible {
		t.Fatalf("expected tab bar hidden while scrollback indicator is visible")
	}
	if !resolved.ScrollbackVisible {
		t.Fatalf("expected scrollback indicator visible")
	}
}

func TestResolveScrollbackSuppressesConnectionBanner(t *testing.T) {
	now := time.Now()
	state := State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
		ConnectionStyle:   BannerRed,
		ScrollbackMessage: "[0%]",
	}
	resolved := Resolve(state, Cursor{Row: 5, Col: 1, Visible: true}, now, ResolveOptions{})
	if !resolved.ScrollbackVisible {
		t.Fatalf("expected scrollback indicator visible")
	}
	if resolved.ConnectionVisible {
		t.Fatalf("expected connection banner suppressed while scrollback is visible")
	}
	row := renderRow(t, ComposeResolved(nil, 120, 20, Cursor{Row: 5, Col: 1, Visible: true}, resolved), 120, 20, 0)
	if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
		t.Fatalf("expected no reconnect text when scrollback owns top row, got row=%q", row)
	}
	if !strings.Contains(row, "[0%]") {
		t.Fatalf("expected scrollback badge on top row, got row=%q", row)
	}
}

func TestResolveLoadingAppearsAfterConnectedExpires(t *testing.T) {
	now := time.Now()
	state := State{
		Theme:               theme.TUI("default"),
		ConnectionMessage:   "connected to https://relay.example/v1",
		ConnectionStyle:     BannerGreen,
		ConnectionShownAt:   now,
		ConnectionExpiresAt: now.Add(2 * time.Second),
		LoadingMessage:      "loading from relay",
	}
	before := Resolve(state, Cursor{Row: 2, Col: 1, Visible: true}, now, ResolveOptions{})
	if before.LoadingVisible {
		t.Fatalf("expected loading banner hidden while connected banner is active")
	}
	after := Resolve(state, Cursor{Row: 2, Col: 1, Visible: true}, now.Add(2500*time.Millisecond), ResolveOptions{})
	if after.ConnectionVisible {
		t.Fatalf("expected connected banner to expire")
	}
	if !after.LoadingVisible {
		t.Fatalf("expected loading banner after connected banner expiry")
	}
	row := renderRow(t, ComposeTopOverlayResolved(120, Cursor{Row: 2, Col: 1, Visible: true}, after), 120, 8, 0)
	if !strings.Contains(row, "loading from relay") {
		t.Fatalf("expected loading banner text, got %q", row)
	}
}

func TestResolveLoadingSuppressedByScrollbackAndDisconnect(t *testing.T) {
	now := time.Now()
	state := State{
		Theme:             theme.TUI("default"),
		LoadingMessage:    "loading from relay",
		ScrollbackMessage: "[50%]",
		DisconnectTitle:   "Not connected",
		DisconnectDetail:  "reconnecting in 2s",
		DisconnectVisible: true,
	}
	resolved := Resolve(state, Cursor{Row: 5, Col: 1, Visible: true}, now, ResolveOptions{})
	if resolved.LoadingVisible {
		t.Fatalf("expected loading banner hidden while scrollback owns top row")
	}
	if !resolved.ScrollbackVisible {
		t.Fatalf("expected scrollback visible")
	}
	state.ScrollbackMessage = ""
	resolved = Resolve(state, Cursor{Row: 5, Col: 1, Visible: true}, now, ResolveOptions{})
	if resolved.LoadingVisible {
		t.Fatalf("expected loading banner hidden while disconnect overlay is visible")
	}
}

func TestComposeResolvedBannerPreservesPromptWhileSuppressingTabs(t *testing.T) {
	const cols, rows = 100, 10
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> ")

	var base bytes.Buffer
	if err := render.SnapshotViewportNoClear(&base, snap, cols, rows); err != nil {
		t.Fatalf("render base: %v", err)
	}

	now := time.Now()
	state := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "tab-a"}},
		ActiveTab:         0,
		ConnectionMessage: "connection lost to https://localhost:1234/v1, reconnecting in 2s",
		ConnectionStyle:   BannerRed,
		ConnectionShownAt: now,
	}
	cursor := Cursor{Row: 1, Col: 8, Visible: true}
	resolved := Resolve(state, cursor, now, ResolveOptions{HideTabsOnTopRow: true})
	out := ComposeResolved(base.Bytes(), cols, rows, cursor, resolved)

	row := renderRow(t, out, cols, rows, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt preserved on row 1 under banner badge, got row=%q", row)
	}
	if !strings.Contains(row, "reconnecting") {
		t.Fatalf("expected banner content on row 1, got row=%q", row)
	}
	if strings.Contains(row, "tab-a") {
		t.Fatalf("expected banner to suppress tab text on row 1, got row=%q", row)
	}
}

func makeSnapshot(cols, rows, cursorX, cursorY int) *protocolpb.Snapshot {
	size := cols * rows
	return &protocolpb.Snapshot{
		Cols:          uint32(cols),
		Rows:          uint32(rows),
		Runes:         make([]uint32, size),
		Modes:         make([]int32, size),
		Fg:            make([]uint32, size),
		Bg:            make([]uint32, size),
		Cursor:        &protocolpb.Cursor{X: uint32(cursorX), Y: uint32(cursorY)},
		CursorVisible: true,
	}
}

func setRow(snap *protocolpb.Snapshot, row int, content string) {
	if snap == nil || row < 0 || row >= int(snap.Rows) {
		return
	}
	cols := int(snap.Cols)
	for i := 0; i < cols; i++ {
		idx := row*cols + i
		if i < len(content) {
			snap.Runes[idx] = uint32(content[i])
		} else {
			snap.Runes[idx] = uint32(' ')
		}
	}
}

func renderRow(t *testing.T, out []byte, cols, rows, row int) string {
	t.Helper()
	e := emu.New(cols, rows)
	if err := e.Write(out); err != nil {
		t.Fatalf("emulator write: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	return rowString(snap, row)
}

func rowString(s terminal.Snapshot, row int) string {
	if row < 0 || row >= s.Rows {
		return ""
	}
	var b strings.Builder
	for x := 0; x < s.Cols; x++ {
		cell := s.Cells[row*s.Cols+x]
		if cell.Mode&terminal.ModeHidden != 0 {
			b.WriteRune(' ')
			continue
		}
		if cell.Grapheme != "" {
			b.WriteString(cell.Grapheme)
			continue
		}
		if cell.Rune == 0 {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(cell.Rune)
	}
	return b.String()
}
