package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/theme"
)

func TestActionRegressionTopOverlayBannerBeatsTabBar(t *testing.T) {
	rt := NewRuntime()
	rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{
		Sources:  []SessionTabSource{{ID: "host-a", Name: "host-a"}},
		ActiveID: "host-a",
	}})
	rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:    StatusConnectionLost,
		Message: "connection lost to https://relay.example/v1, reconnecting",
	}})

	state := rt.Read()
	resolved := Resolve(state, Cursor{Row: 2, Col: 1, Visible: true}, time.Now(), ResolveOptions{})
	row := renderRow(t, ComposeTopOverlayResolved(80, Cursor{Row: 2, Col: 1, Visible: true}, resolved), 80, 8, 0)
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected reconnect banner on top row, got %q", row)
	}
	if !strings.Contains(row, "host-a") {
		t.Fatalf("expected tab text to remain visible left of banner badge, got %q", row)
	}
}

func TestActionRegressionClearOverlaysRepaintsPromptRow(t *testing.T) {
	const cols, rows = 60, 10
	base := makeSnapshot(cols, rows, 0, 0)
	setRow(base, 0, "PROMPT> ")
	next := makeSnapshot(cols, rows, 0, 0)
	setRow(next, 0, "PROMPT> ")
	cursor := Cursor{Row: 1, Col: 8, Visible: true}
	overlayState := State{
		Theme:             theme.TUI("default"),
		ConnectionMessage: "connection lost",
		ConnectionStyle:   BannerRed,
	}
	prevResolved := Resolve(overlayState, cursor, time.Now(), ResolveOptions{})
	prev, err := ComposeViewportSnapshot(base, cols, rows, prevResolved, cursor)
	if err != nil {
		t.Fatalf("compose previous frame: %v", err)
	}
	prevOut, err := RenderHost(HostRenderInput{
		Snapshot:  base,
		Cols:      cols,
		Rows:      rows,
		Cursor:    cursor,
		State:     overlayState,
		Now:       time.Now(),
		ForceFull: true,
	})
	if err != nil {
		t.Fatalf("render previous host frame: %v", err)
	}

	rt := NewRuntime()
	rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:    StatusConnectionLost,
		Message: "connection lost",
	}})
	rt.ApplyAction(ClearOverlaysAction{})

	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       cursor,
		State:        rt.Read(),
		Now:          time.Now(),
	})
	if err != nil {
		t.Fatalf("render host: %v", err)
	}
	e := emu.New(cols, rows)
	if err := e.Write(prevOut.Bytes); err != nil {
		t.Fatalf("emulator write previous: %v", err)
	}
	if err := e.Write(out.Bytes); err != nil {
		t.Fatalf("emulator write next: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	row := rowString(snap, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt row repaint after clear overlays, got %q", row)
	}
}

func TestActionRegressionDeltaSingleCharNoFullRow(t *testing.T) {
	const cols, rows = 40, 8
	prev := makeSnapshot(cols, rows, 0, 1)
	setRow(prev, 1, "abcdef")
	next := makeSnapshot(cols, rows, 0, 1)
	setRow(next, 1, "abZdef")

	rt := NewRuntime()
	rt.ApplyAction(ContextAction{Input: ContextInput{Theme: theme.TUI("default")}})
	out, err := RenderHost(HostRenderInput{
		PrevSnapshot: prev,
		Snapshot:     next,
		Cols:         cols,
		Rows:         rows,
		Cursor:       Cursor{Row: 2, Col: 1, Visible: true},
		State:        rt.Read(),
		Now:          time.Now(),
		ForceFull:    false,
		Frame:        FrameState{},
	})
	if err != nil {
		t.Fatalf("render host delta: %v", err)
	}
	raw := string(out.Bytes)
	if !strings.Contains(raw, "\x1b[2;3H") {
		t.Fatalf("expected changed-cell delta cursor, raw=%q", raw)
	}
	if strings.Contains(raw, "\x1b[2;1Habcdef") {
		t.Fatalf("expected no full-row repaint for one-char delta, raw=%q", raw)
	}
}

func TestActionRegressionAttachBannerPreservesPromptLeftOfBadge(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT> visible with badge")

	rt := NewRuntime()
	rt.ApplyAction(StatusAction{Input: StatusInput{
		Kind:    StatusConnectionLost,
		Message: "connection lost",
	}})
	out, err := RenderAttach(AttachRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 1, Col: 1, Visible: true},
		State:    rt.Read(),
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("render attach: %v", err)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt to remain visible on row 1 with banner badge, got %q", row)
	}
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected banner badge on row 1, got %q", row)
	}
}
