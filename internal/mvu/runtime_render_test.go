package mvu

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimeRenderHostFrameUsesRuntimeStateAndCache(t *testing.T) {
	const cols, rows = 80, 8
	runtime := NewRuntime()
	runtime.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "host-a", Name: "host-a"}}, ActiveID: "host-a"}})
	runtime.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnected, Message: "connected to relay", Duration: 2 * time.Second}})

	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT>")
	cursor := CursorFromSnapshot(snap, cols, rows)
	cache := &RenderCache{}

	out, err := runtime.RenderHostFrame(RuntimeHostFrameInput{
		Snapshot:  snap,
		Cols:      cols,
		Rows:      rows,
		Cursor:    cursor,
		Now:       time.Now(),
		Cache:     cache,
		ForceFull: false,
	})
	if err != nil {
		t.Fatalf("RenderHostFrame: %v", err)
	}
	row := renderRow(t, out.Rendered.Bytes, cols, rows, 0)
	if !strings.Contains(row, "connected") {
		t.Fatalf("expected connection banner on row 1, got %q", row)
	}
	if cache.PrevSnapshot == nil {
		t.Fatalf("expected render cache commit")
	}
	if out.StateDelay <= 0 {
		t.Fatalf("expected positive state expiry delay, got %v", out.StateDelay)
	}
}

func TestRuntimeRenderAttachFrameRespectsSuppressTabsAndCommitSnapshot(t *testing.T) {
	const cols, rows = 80, 8
	runtime := NewRuntime()
	runtime.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "remote-a", Name: "remote-a"}}, ActiveID: "remote-a"}})

	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT>")
	cursor := CursorFromSnapshot(snap, cols, rows)
	cache := &RenderCache{}

	out, err := runtime.RenderAttachFrame(RuntimeAttachFrameInput{
		Snapshot:     snap,
		Cols:         cols,
		Rows:         rows,
		Cursor:       cursor,
		Now:          time.Now(),
		SuppressTabs: true,
		Cache:        cache,
	})
	if err != nil {
		t.Fatalf("RenderAttachFrame: %v", err)
	}
	row := renderRow(t, out.Rendered.Bytes, cols, rows, 0)
	if strings.Contains(row, "remote-a") {
		t.Fatalf("expected tabs suppressed on row 1, got %q", row)
	}
	if cache.PrevSnapshot == nil {
		t.Fatalf("expected composed snapshot committed to cache")
	}
}

func TestRuntimeRenderAttachFrameForceTabsVisibleOnTopRow(t *testing.T) {
	const cols, rows = 80, 8
	runtime := NewRuntime()
	runtime.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "remote-a", Name: "remote-a"}}, ActiveID: "remote-a"}})

	snap := makeSnapshot(cols, rows, 0, 0)
	setRow(snap, 0, "PROMPT>")
	cursor := CursorFromSnapshot(snap, cols, rows)
	cache := &RenderCache{}

	out, err := runtime.RenderAttachFrame(RuntimeAttachFrameInput{
		Snapshot:         snap,
		Cols:             cols,
		Rows:             rows,
		Cursor:           cursor,
		Now:              time.Now(),
		ForceTabsVisible: true,
		Cache:            cache,
	})
	if err != nil {
		t.Fatalf("RenderAttachFrame: %v", err)
	}
	row := renderRow(t, out.Rendered.Bytes, cols, rows, 0)
	if !strings.Contains(row, "remote-a") {
		t.Fatalf("expected force-tabs render to keep tab bar visible, got %q", row)
	}
}

func TestRuntimeRenderDisabledFrameCommitsDisabledCache(t *testing.T) {
	const cols, rows = 60, 6
	runtime := NewRuntime()
	runtime.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "disabled", Name: "disabled"}}, ActiveID: "disabled"}})

	snap := makeSnapshot(cols, rows, 0, 0)
	cursor := Cursor{Row: 1, Col: 1, Visible: false}
	cache := &RenderCache{}

	out, err := runtime.RenderDisabledFrame(RuntimeDisabledFrameInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   cursor,
		Now:      time.Now(),
		Cache:    cache,
	})
	if err != nil {
		t.Fatalf("RenderDisabledFrame: %v", err)
	}
	if len(out.Rendered.Bytes) == 0 {
		t.Fatalf("expected disabled render bytes")
	}
	if cache.PrevSnapshot == nil {
		t.Fatalf("expected disabled render to commit composed snapshot")
	}
}
