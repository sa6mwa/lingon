package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/theme"
)

func TestRenderHostForceFullIncludesClearSequence(t *testing.T) {
	const cols, rows = 40, 8
	snap := makeSnapshot(cols, rows, 0, 1)
	setRow(snap, 1, "host-frame")
	out, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: false,
		},
		Now:       time.Now(),
		ForceFull: true,
		Frame:     FrameState{},
	})
	if err != nil {
		t.Fatalf("render host full: %v", err)
	}
	if !strings.Contains(string(out.Bytes), "\x1b[2J") {
		t.Fatalf("expected full host render to include clear-screen sequence")
	}
}

func TestRenderHostDeltaEmitsChangedCellSpan(t *testing.T) {
	const cols, rows = 40, 8
	prev := makeSnapshot(cols, rows, 0, 1)
	setRow(prev, 1, "abcdef")
	next := makeSnapshot(cols, rows, 0, 1)
	setRow(next, 1, "abZdef")

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: false,
		},
		Now:       time.Now(),
		ForceFull: false,
		Frame:     FrameState{},
	})
	if err != nil {
		t.Fatalf("render host delta: %v", err)
	}
	raw := string(out.Bytes)
	if !strings.Contains(raw, "\x1b[2;3H") {
		t.Fatalf("expected delta span to target changed column, raw=%q", raw)
	}
	if strings.Contains(raw, "\x1b[2;1Habcdef") {
		t.Fatalf("expected no full-row repaint for single-cell change, raw=%q", raw)
	}
}

func TestRenderHostMasksTopRowWhenOverlayHides(t *testing.T) {
	const cols, rows = 60, 10
	base := makeSnapshot(cols, rows, 0, 0)
	setRow(base, 0, "PROMPT> ")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> ")
	cursor := Cursor{Row: 1, Col: 8, Visible: true}
	prevResolved := Resolve(State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}, cursor, time.Now(), ResolveOptions{})
	prev, err := ComposeViewportSnapshot(base, cols, rows, prevResolved, cursor)
	if err != nil {
		t.Fatalf("compose previous frame: %v", err)
	}

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       cursor,
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: false,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host overlay-clear: %v", err)
	}
	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: base,
		Cols:     cols,
		Rows:     rows,
		Cursor:   cursor,
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render host overlay prev full: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(out.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	snapOut, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row := rowString(snapOut, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt row to be repainted, got row=%q", row)
	}
	if strings.Contains(row, "connection lost") {
		t.Fatalf("expected reconnect banner cleared after overlay hide, got row=%q", row)
	}
}

func TestRenderAttachForceFullIncludesClearSequence(t *testing.T) {
	const cols, rows = 40, 8
	snap := makeSnapshot(cols, rows, 0, 1)
	setRow(snap, 1, "attach-frame")
	out, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: false,
		},
		Now:               time.Now(),
		ForceFull:         true,
		ScrollbackVisible: false,
		Frame:             FrameState{},
	})
	if err != nil {
		t.Fatalf("render attach full: %v", err)
	}
	if !strings.Contains(string(out.Bytes), "\x1b[2J") {
		t.Fatalf("expected full attach render to include clear-screen sequence")
	}
}

func TestRenderAttachMasksTopRowWhenOverlayHides(t *testing.T) {
	const cols, rows = 60, 10
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "PROMPT> ")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> ")

	out, err := RenderAttach(AttachRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 8, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: false,
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
		Frame: FrameState{
			LastTopOverlayVisible: true,
		},
	})
	if err != nil {
		t.Fatalf("render attach overlay-clear: %v", err)
	}
	if !strings.Contains(string(out.Bytes), "\x1b[1;1H") {
		t.Fatalf("expected row-1 repaint after top-overlay hide")
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt row to remain visible, got %q", row)
	}
}

func TestRenderAttachConnectionBannerOwnsTopRow(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> should stay visible")

	out, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render attach banner: %v", err)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected attach banner to own row 1 without prompt bleed, got %q", row)
	}
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected attach banner text on top row, got %q", row)
	}
}

func TestComposeTopOverlayResolvedBannerOverwritesTabRow(t *testing.T) {
	state := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "tab-a"}},
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}
	resolved := Resolve(state, Cursor{Row: 2, Col: 1, Visible: true}, time.Now(), ResolveOptions{})
	out := ComposeTopOverlayResolved(80, Cursor{Row: 2, Col: 1, Visible: true}, resolved)
	row := renderRow(t, out, 80, 8, 0)
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected banner to occupy row 1, got %q", row)
	}
}

func TestComposeTopOverlayResolvedKeepsCursorPositionOnBannerRow(t *testing.T) {
	state := State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}
	resolved := Resolve(state, Cursor{Row: 1, Col: 10, Visible: true}, time.Now(), ResolveOptions{})
	out := ComposeTopOverlayResolved(80, Cursor{Row: 1, Col: 10, Visible: true}, resolved)
	raw := string(out)
	if !strings.Contains(raw, "\x1b[?25h") {
		t.Fatalf("expected cursor visible while typing under top overlay, raw=%q", raw)
	}
	if !strings.Contains(raw, "\x1b[1;10H") {
		t.Fatalf("expected cursor to remain at terminal row/col, raw=%q", raw)
	}
}

func TestComposeTopOverlayResolvedBannerDoesNotClearEntireRow(t *testing.T) {
	state := State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}
	resolved := Resolve(state, Cursor{Row: 1, Col: 1, Visible: true}, time.Now(), ResolveOptions{})
	out := ComposeTopOverlayResolved(80, Cursor{Row: 1, Col: 1, Visible: true}, resolved)
	raw := string(out)
	if strings.Contains(raw, "\x1b[2K") {
		t.Fatalf("expected reconnect banner overlay to preserve base row content, raw=%q", raw)
	}
}

func TestComposeTopOverlayResolvedBannerDoesNotFloodEntireRowBackground(t *testing.T) {
	const cols = 80
	state := State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}
	resolved := Resolve(state, Cursor{Row: 1, Col: 1, Visible: true}, time.Now(), ResolveOptions{})
	out := ComposeTopOverlayResolved(cols, Cursor{Row: 1, Col: 1, Visible: true}, resolved)
	raw := string(out)
	flood := "\x1b[97;41m" + strings.Repeat(" ", cols)
	if strings.Contains(raw, flood) {
		t.Fatalf("expected banner background limited to message span, raw=%q", raw)
	}
}

func TestRenderHostSuppressTabsKeepsBaseRowWhenBannerHidden(t *testing.T) {
	const cols, rows = 80, 10
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> ")

	out, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 8, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
			Tabs:          []Tab{{Index: 1, Title: "tab-a"}},
		},
		Now:     time.Now(),
		Resolve: ResolveOptions{SuppressTabs: true},
		Frame:   FrameState{},
	})
	if err != nil {
		t.Fatalf("render host suppress tabs: %v", err)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt row visible when tabs suppressed, got %q", row)
	}
}

func TestRenderHostConnectionBannerPreservesPromptLeftOfBadge(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> should stay visible")

	out, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host banner: %v", err)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt to remain visible with host banner badge, got %q", row)
	}
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected banner text on top row, got %q", row)
	}
}

func TestRenderHostConnectionBannerOverwritesTopRowWithoutShiftingContent(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> shifted-under-banner")
	setRow(snap, 1, "SECOND-ROW")

	out, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host banner: %v", err)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row0, "connection lost") {
		t.Fatalf("expected banner on row 1, got %q", row0)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if !strings.Contains(row1, "SECOND-ROW") {
		t.Fatalf("expected row 2 to remain unchanged under overlay semantics, got %q", row1)
	}
}

func TestRenderHostConnectionBannerSkipsBaseTopRowDelta(t *testing.T) {
	const cols, rows = 80, 8
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "PROMPT> old under banner")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> new under banner")

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host banner delta: %v", err)
	}
	raw := string(out.Bytes)
	if strings.Contains(raw, "old under banner") {
		t.Fatalf("expected host delta to skip stale base row 1 while banner visible, raw=%q", raw)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected host banner text on top row, got %q", row)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if strings.Contains(row1, "new") {
		t.Fatalf("expected top-row base content to stay on row 1 (no shift), got row2=%q", row1)
	}
}

func TestRenderHostTopOverlayWithoutBaselineSkipsBaseRowOnePaint(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> hidden-under-tabs")
	setRow(snap, 1, "SECOND-ROW-STABLE")

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: nil,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
			Tabs: []Tab{
				{Index: 1, Title: "tab-a"},
				{Index: 2, Title: "tab-b"},
			},
			ActiveTab: 0,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host without baseline: %v", err)
	}
	raw := string(out.Bytes)
	if strings.Contains(raw, "\x1b[1;1H\x1b[0;39;49m") {
		t.Fatalf("expected top-overlay frame to avoid base row-1 repaint prefix, raw=%q", raw)
	}
	if !strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("expected top-overlay writer to own row 1 render, raw=%q", raw)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row0, "tab-a") {
		t.Fatalf("expected tab bar on row 1, got %q", row0)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if !strings.Contains(row1, "SECOND-ROW-STABLE") {
		t.Fatalf("expected row 2 unchanged under top-overlay render, got %q", row1)
	}
}

func TestRenderHostReconnectBannerClearsStaleBodyRows(t *testing.T) {
	const cols, rows = 80, 8
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "PROMPT> old")
	setRow(prev, 1, "STALE-LINE")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> new")
	setRow(next, 1, "")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: prev,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	raw := string(nextOut.Bytes)
	if strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("expected no full-screen clear for top-overlay transition, raw=%q", raw)
	}
	if strings.Contains(raw, "SECOND-ROW-STABLE") {
		t.Fatalf("expected unchanged body rows excluded from reconnect delta, raw=%q", raw)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row1 := rowString(snap, 1)
	if strings.Contains(row1, "STALE-LINE") {
		t.Fatalf("expected stale body row cleared on reconnect render, got %q", row1)
	}
}

func TestRenderHostConnectionBannerClearsStaleTopRowOnTransition(t *testing.T) {
	const cols, rows = 100, 12
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "STALE_TOP_ROW_TOKEN_HOST")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "NEW_TOP_ROW_TOKEN_HOST")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: prev,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	raw := string(nextOut.Bytes)
	if strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("expected no full-screen clear for scrollback overlay transition, raw=%q", raw)
	}
	if strings.Contains(raw, "SECOND-ROW-STABLE") {
		t.Fatalf("expected unchanged body rows excluded from scrollback delta, raw=%q", raw)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(snap, 0)
	if strings.Contains(row0, "STALE_TOP_ROW_TOKEN_HOST") {
		t.Fatalf("expected stale top-row token cleared on transition, got %q", row0)
	}
	if !strings.Contains(row0, "NEW_TOP_ROW_TOKEN_HOST") {
		t.Fatalf("expected current base top-row token preserved with banner badge, got %q", row0)
	}
	if !strings.Contains(row0, "connection lost") || !strings.Contains(row0, "reconnecting") {
		t.Fatalf("expected reconnect banner on top row, got %q", row0)
	}
}

func TestRenderHostConnectionBannerTransitionDoesNotShiftUnchangedContent(t *testing.T) {
	const cols, rows = 100, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")
	setRow(snap, 1, "SECOND-ROW-STABLE")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: snap,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	raw := string(nextOut.Bytes)
	if strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("expected no full-screen clear for top-overlay transition, raw=%q", raw)
	}
	if strings.Contains(raw, "SECOND-ROW-STABLE") {
		t.Fatalf("expected unchanged body rows excluded from reconnect delta, raw=%q", raw)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(got, 0)
	if !strings.Contains(row0, "connection lost") || !strings.Contains(row0, "reconnecting") {
		t.Fatalf("expected reconnect banner on top row, got %q", row0)
	}
	row1 := rowString(got, 1)
	if !strings.Contains(row1, "SECOND-ROW-STABLE") {
		t.Fatalf("expected row 2 unchanged (no top-row shift), got %q", row1)
	}
	row2 := rowString(got, 2)
	if strings.Contains(row2, "SECOND-ROW-STABLE") || strings.Contains(row2, "PROMPT> stable-top-row") {
		t.Fatalf("expected no downward row shift during reconnect overlay, got row3=%q", row2)
	}
}

func TestRenderHostScrollbackIndicatorKeepsBaseTopRowOnTransition(t *testing.T) {
	const cols, rows = 100, 12
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "STALE_TOP_ROW_TOKEN_SCROLL")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "NEW_TOP_ROW_TOKEN_SCROLL")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: prev,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ScrollbackMessage: "[83%]",
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(snap, 0)
	if strings.Contains(row0, "STALE_TOP_ROW_TOKEN_SCROLL") {
		t.Fatalf("expected updated base top-row text, got %q", row0)
	}
	if !strings.Contains(row0, "NEW_TOP_ROW_TOKEN_SCROLL") {
		t.Fatalf("expected scrollback indicator to preserve base row content, got %q", row0)
	}
	if got := row0[cols-len("[83%]"):]; got != "[83%]" {
		t.Fatalf("expected scrollback indicator right-aligned, got suffix=%q row=%q", got, row0)
	}
}

func TestRenderHostScrollbackBannerTransitionDoesNotShiftUnchangedContent(t *testing.T) {
	const cols, rows = 100, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")
	setRow(snap, 1, "SECOND-ROW-STABLE")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: snap,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ScrollbackMessage: "[83%]",
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(got, 0)
	if !strings.Contains(row0, "[83%]") {
		t.Fatalf("expected scrollback banner on top row, got %q", row0)
	}
	row1 := rowString(got, 1)
	if !strings.Contains(row1, "SECOND-ROW-STABLE") {
		t.Fatalf("expected row 2 unchanged (no top-row shift), got %q", row1)
	}
	row2 := rowString(got, 2)
	if strings.Contains(row2, "SECOND-ROW-STABLE") || strings.Contains(row2, "PROMPT> stable-top-row") {
		t.Fatalf("expected no downward row shift during scrollback overlay, got row3=%q", row2)
	}
}

func TestRenderHostScrollbackSuppressesReconnectBannerAndRemainsRightAligned(t *testing.T) {
	const cols, rows = 120, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")

	out, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 20, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
			ScrollbackMessage: "[0%]",
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render host scrollback+reconnect: %v", err)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
		t.Fatalf("expected reconnect banner suppressed while scrollback visible, got %q", row0)
	}
	if !strings.Contains(row0, "[0%]") {
		t.Fatalf("expected scrollback indicator on top row, got %q", row0)
	}
	if got := row0[cols-len("[0%]"):]; got != "[0%]" {
		t.Fatalf("expected [0%%] right-aligned, got suffix=%q row=%q", got, row0)
	}
}

func TestRenderHostScrollbackIndicatorTransitionFrom100To0PreservesBaseAndAlignment(t *testing.T) {
	const cols, rows = 120, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	base := []byte(strings.Repeat(".", cols))
	base[cols-6] = 'x'
	base[cols-5] = 'y'
	setRow(snap, 0, string(base))

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ScrollbackMessage: "[100%]",
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}

	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: prevOut.ComposedSnapshot,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ScrollbackMessage: "[0%]",
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}

	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(got, 0)
	wantSuffix := "xy[0%]"
	if gotSuffix := row0[cols-len(wantSuffix):]; gotSuffix != wantSuffix {
		t.Fatalf("expected indicator transition suffix=%q, got suffix=%q row=%q", wantSuffix, gotSuffix, row0)
	}
	if strings.Contains(row0, "[[0%]") {
		t.Fatalf("expected no stale bracket bleed on [100%%]->[0%%] transition, row=%q", row0)
	}
}

func TestRenderHostConnectionBannerTransitionKeepsTerminalCursorOnRow1(t *testing.T) {
	const cols, rows = 100, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")

	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 12, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: snap,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 12, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	if got.Cursor.Y != 0 {
		t.Fatalf("expected terminal cursor to stay on row 1 during reconnect banner transition, got row=%d col=%d", got.Cursor.Y+1, got.Cursor.X+1)
	}
}

func TestRenderHostTabBarStatusLengthShrinkDoesNotColorPadPrefix(t *testing.T) {
	const cols, rows = 120, 10
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")

	first, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
			Tabs: []Tab{
				{Index: 1, Title: "tab-a"},
				{Index: 2, Title: "tab-b"},
			},
			ActiveTab:         0,
			ConnectionMessage: "connected to https://localhost:12843/v1",
			ConnectionStyle:   BannerGreen,
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render first: %v", err)
	}

	second, err := RenderHost(HostRenderInput{
		PrevSnapshot: first.ComposedSnapshot,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
			Tabs: []Tab{
				{Index: 1, Title: "tab-a"},
				{Index: 2, Title: "tab-b"},
			},
			ActiveTab:         0,
			ConnectionMessage: "wall inactivity 10s",
			ConnectionStyle:   BannerGreen,
		},
		Now:   time.Now(),
		Frame: first.Frame,
	})
	if err != nil {
		t.Fatalf("render second: %v", err)
	}

	raw := string(second.Bytes)
	if strings.Contains(raw, "\x1b[97;42m wall inactivity 10s") {
		t.Fatalf("expected no banner-colored left padding when status shrinks, raw=%q", raw)
	}
	if strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("expected no full tab-row repaint on status tick, raw=%q", raw)
	}
}

func TestRenderHostConnectionBannerTransitionKeepsTerminalCursorColumn(t *testing.T) {
	const cols, rows = 100, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")

	const wantCol = 16
	prevOut, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: wantCol, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderHost(HostRenderInput{
		PrevSnapshot: snap,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: wantCol, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	if got.Cursor.Y != 0 {
		t.Fatalf("expected terminal cursor row 1 under top overlay, got row=%d col=%d", got.Cursor.Y+1, got.Cursor.X+1)
	}
	if got.Cursor.X+1 != wantCol {
		t.Fatalf("expected terminal cursor column %d under top overlay, got row=%d col=%d", wantCol, got.Cursor.Y+1, got.Cursor.X+1)
	}
}

func TestRenderAttachConnectionBannerSkipsBaseTopRowDelta(t *testing.T) {
	const cols, rows = 80, 8
	prev := makeSnapshot(cols, rows, 0, 0)
	setRow(prev, 0, "PROMPT> old under banner")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> new under banner")

	out, err := RenderAttach(AttachRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
	})
	if err != nil {
		t.Fatalf("render attach banner delta: %v", err)
	}
	raw := string(out.Bytes)
	if strings.Contains(raw, "old under banner") {
		t.Fatalf("expected attach delta to skip stale base row 1 while banner visible, raw=%q", raw)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected attach banner text on top row, got %q", row)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if strings.Contains(row1, "new") {
		t.Fatalf("expected top-row base content to stay on row 1 (no shift), got row2=%q", row1)
	}
}

func TestRenderAttachTopOverlayWithoutBaselineSkipsBaseRowOnePaint(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> hidden-under-tabs")
	setRow(snap, 1, "SECOND-ROW-STABLE")

	out, err := RenderAttach(AttachRenderInput{
		PrevSnapshot: nil,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:         theme.TUI("default"),
			TabBarVisible: true,
			Tabs: []Tab{
				{Index: 1, Title: "tab-a"},
				{Index: 2, Title: "tab-b"},
			},
			ActiveTab: 0,
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
	})
	if err != nil {
		t.Fatalf("render attach without baseline: %v", err)
	}
	raw := string(out.Bytes)
	if strings.Contains(raw, "\x1b[1;1H\x1b[0;39;49m") {
		t.Fatalf("expected top-overlay frame to avoid base row-1 repaint prefix, raw=%q", raw)
	}
	if !strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("expected top-overlay writer to own row 1 render, raw=%q", raw)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row0, "tab-a") {
		t.Fatalf("expected tab bar on row 1, got %q", row0)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if !strings.Contains(row1, "SECOND-ROW-STABLE") {
		t.Fatalf("expected row 2 unchanged under top-overlay render, got %q", row1)
	}
}

func TestRenderAttachConnectionBannerOverwritesTopRowWithoutShiftingContent(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> shifted-under-banner")
	setRow(snap, 1, "SECOND-ROW")

	out, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
	})
	if err != nil {
		t.Fatalf("render attach banner: %v", err)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row0, "connection lost") {
		t.Fatalf("expected banner on row 1, got %q", row0)
	}
	row1 := renderRow(t, out.Bytes, cols, rows, 1)
	if !strings.Contains(row1, "SECOND-ROW") {
		t.Fatalf("expected row 2 to remain unchanged under overlay semantics, got %q", row1)
	}
}

func TestRenderAttachConnectionBannerTransitionDoesNotShiftUnchangedContent(t *testing.T) {
	const cols, rows = 100, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")
	setRow(snap, 1, "SECOND-ROW-STABLE")

	prevOut, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
		ForceFull:         true,
	})
	if err != nil {
		t.Fatalf("render prev full: %v", err)
	}
	nextOut, err := RenderAttach(AttachRenderInput{
		PrevSnapshot: snap,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
	})
	if err != nil {
		t.Fatalf("render next delta: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write prev: %v", err)
	}
	if err := e.Write(nextOut.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	got, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row0 := rowString(got, 0)
	if !strings.Contains(row0, "connection lost") || !strings.Contains(row0, "reconnecting") {
		t.Fatalf("expected reconnect banner on top row, got %q", row0)
	}
	row1 := rowString(got, 1)
	if !strings.Contains(row1, "SECOND-ROW-STABLE") {
		t.Fatalf("expected row 2 unchanged (no top-row shift), got %q", row1)
	}
	row2 := rowString(got, 2)
	if strings.Contains(row2, "SECOND-ROW-STABLE") || strings.Contains(row2, "PROMPT> stable-top-row") {
		t.Fatalf("expected no downward row shift during reconnect overlay, got row3=%q", row2)
	}
}

func TestRenderHostReconnectCountdownDeltaSkipsTabBarRepaint(t *testing.T) {
	const cols, rows = 120, 24
	snap := makeSnapshot(cols, rows, 0, 1)
	setRow(snap, 0, "PROMPT> host")
	setRow(snap, 1, "ROW2")

	state1 := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "host-a"}, {Index: 2, Title: "host-b"}},
		ActiveTab:         0,
		ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 3s",
		ConnectionStyle:   BannerRed,
	}
	first, err := RenderHost(HostRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: true},
		State:    state1,
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("render host first reconnect frame: %v", err)
	}

	state2 := state1
	state2.ConnectionMessage = "connection lost to https://localhost:12843/v1, reconnecting in 2s"
	second, err := RenderHost(HostRenderInput{
		PrevSnapshot: first.ComposedSnapshot,
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State:        state2,
		Now:          time.Now(),
		Frame:        first.Frame,
	})
	if err != nil {
		t.Fatalf("render host second reconnect frame: %v", err)
	}
	raw := string(second.Bytes)
	if strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("expected reconnect countdown delta to avoid full tab row repaint, raw=%q", raw)
	}
}

func TestRenderAttachReconnectCountdownDeltaSkipsTabBarRepaint(t *testing.T) {
	const cols, rows = 120, 24
	snap := makeSnapshot(cols, rows, 0, 1)
	setRow(snap, 0, "PROMPT> attach")
	setRow(snap, 1, "ROW2")

	state1 := State{
		Theme:             theme.TUI("default"),
		TabBarVisible:     true,
		Tabs:              []Tab{{Index: 1, Title: "attach-a"}, {Index: 2, Title: "attach-b"}},
		ActiveTab:         0,
		ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 3s",
		ConnectionStyle:   BannerRed,
	}
	first, err := RenderAttach(AttachRenderInput{
		Snapshot:          snap,
		Cols:              cols,
		Rows:              rows,
		Cursor:            Cursor{Row: 2, Col: 1, Visible: true},
		State:             state1,
		Now:               time.Now(),
		ScrollbackVisible: false,
	})
	if err != nil {
		t.Fatalf("render attach first reconnect frame: %v", err)
	}

	state2 := state1
	state2.ConnectionMessage = "connection lost to https://localhost:12843/v1, reconnecting in 2s"
	second, err := RenderAttach(AttachRenderInput{
		PrevSnapshot:      first.ComposedSnapshot,
		Snapshot:          snap,
		Cols:              cols,
		Rows:              rows,
		Cursor:            Cursor{Row: 2, Col: 1, Visible: true},
		State:             state2,
		Now:               time.Now(),
		ScrollbackVisible: false,
		Frame:             first.Frame,
	})
	if err != nil {
		t.Fatalf("render attach second reconnect frame: %v", err)
	}
	raw := string(second.Bytes)
	if strings.Contains(raw, "\x1b[1;1H\x1b[0m\x1b[2K") {
		t.Fatalf("expected reconnect countdown delta to avoid full tab row repaint, raw=%q", raw)
	}
}

func TestRenderHostResizeDeltaDoesNotClearScreen(t *testing.T) {
	const cols0, rows0 = 80, 12
	const cols1, rows1 = 100, 16

	prevSnap := makeSnapshot(cols0, rows0, 0, 1)
	setRow(prevSnap, 0, "PROMPT> host-resize")
	setRow(prevSnap, 1, "ROW2")

	nextSnap := makeSnapshot(cols1, rows1, 0, 1)
	setRow(nextSnap, 0, "PROMPT> host-resize")
	setRow(nextSnap, 1, "ROW2")

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prevSnap,
		Snapshot:     nextSnap,
		Cols:         cols1,
		Rows:         rows1,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now: time.Now(),
		Frame: FrameState{
			LastRenderCols: cols0,
			LastRenderRows: rows0,
			LastSnapCols:   cols0,
			LastSnapRows:   rows0,
		},
	})
	if err != nil {
		t.Fatalf("render host resize delta: %v", err)
	}
	if strings.Contains(string(out.Bytes), "\x1b[2J") {
		t.Fatalf("expected host resize delta to avoid clear-screen sequence, raw=%q", string(out.Bytes))
	}
}

func TestRenderAttachResizeDeltaDoesNotClearScreen(t *testing.T) {
	const cols0, rows0 = 80, 12
	const cols1, rows1 = 100, 16

	prevSnap := makeSnapshot(cols0, rows0, 0, 1)
	setRow(prevSnap, 0, "PROMPT> attach-resize")
	setRow(prevSnap, 1, "ROW2")

	nextSnap := makeSnapshot(cols1, rows1, 0, 1)
	setRow(nextSnap, 0, "PROMPT> attach-resize")
	setRow(nextSnap, 1, "ROW2")

	out, err := RenderAttach(AttachRenderInput{
		PrevSnapshot: prevSnap,
		Snapshot:     nextSnap,
		Cols:         cols1,
		Rows:         rows1,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State: State{
			Theme: theme.TUI("default"),
		},
		Now:               time.Now(),
		ScrollbackVisible: false,
		Frame: FrameState{
			LastRenderCols: cols0,
			LastRenderRows: rows0,
			LastSnapCols:   cols0,
			LastSnapRows:   rows0,
		},
	})
	if err != nil {
		t.Fatalf("render attach resize delta: %v", err)
	}
	if strings.Contains(string(out.Bytes), "\x1b[2J") {
		t.Fatalf("expected attach resize delta to avoid clear-screen sequence, raw=%q", string(out.Bytes))
	}
}

func TestRenderAttachScrollbackSuppressesReconnectBannerAndRemainsRightAligned(t *testing.T) {
	const cols, rows = 120, 12
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> stable-top-row")

	out, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 20, Visible: true},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost to https://localhost:12843/v1, reconnecting in 2s",
			ConnectionStyle:   BannerRed,
			ScrollbackMessage: "[0%]",
		},
		Now:               time.Now(),
		ScrollbackVisible: true,
	})
	if err != nil {
		t.Fatalf("render attach scrollback+reconnect: %v", err)
	}
	row0 := renderRow(t, out.Bytes, cols, rows, 0)
	if strings.Contains(row0, "connection lost") || strings.Contains(row0, "reconnecting") {
		t.Fatalf("expected reconnect banner suppressed while scrollback visible, got %q", row0)
	}
	if !strings.Contains(row0, "[0%]") {
		t.Fatalf("expected scrollback indicator on top row, got %q", row0)
	}
	if got := row0[cols-len("[0%]"):]; got != "[0%]" {
		t.Fatalf("expected [0%%] right-aligned, got suffix=%q row=%q", got, row0)
	}
}
