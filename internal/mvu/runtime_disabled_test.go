package mvu

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/theme"
)

func TestRenderDisabledIncludesBannerOverlay(t *testing.T) {
	const cols, rows = 80, 8
	snap := makeSnapshot(cols, rows, 0, 1)
	setRow(snap, 0, "PROMPT> hidden under banner")
	setRow(snap, 1, "body")

	out, err := RenderDisabled(DisabledRenderInput{
		Snapshot: snap,
		Cols:     cols,
		Rows:     rows,
		Cursor:   Cursor{Row: 2, Col: 1, Visible: false},
		State: State{
			Theme:             theme.TUI("default"),
			ConnectionMessage: "connection lost",
			ConnectionStyle:   BannerRed,
		},
		Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("render disabled: %v", err)
	}
	row := renderRow(t, out.Bytes, cols, rows, 0)
	if !strings.Contains(row, "connection lost") {
		t.Fatalf("expected banner on top row, got %q", row)
	}
	if !strings.Contains(row, "PROMPT>") {
		t.Fatalf("expected prompt to remain visible left of banner badge, got %q", row)
	}
}

func TestRenderDisabledNilSnapshot(t *testing.T) {
	out, err := RenderDisabled(DisabledRenderInput{})
	if err != nil {
		t.Fatalf("render disabled nil snapshot: %v", err)
	}
	if len(out.Bytes) != 0 {
		t.Fatalf("expected empty output for nil snapshot")
	}
}
