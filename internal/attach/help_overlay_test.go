package attach

import (
	"bytes"
	"testing"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestHelpOverlayPersistsAcrossRenders(t *testing.T) {
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Stdout:    &buf,
		TermSize: func() (int, int) {
			return 80, 24
		},
		compositor: mvu.NewRuntime(),
	}
	client.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		SessionID: client.SessionID,
		Endpoint:  client.Endpoint,
	}})
	client.compositor.ApplyAction(mvu.HelpVisibleAction{Visible: true})

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Runes:         make([]uint32, 80*24),
		Modes:         make([]int32, 80*24),
		Fg:            make([]uint32, 80*24),
		Bg:            make([]uint32, 80*24),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}

	client.renderSnapshot(snap)
	client.renderSnapshot(snap)

	helpLines := mvu.HelpLines(client.compositor.Read())
	wrapped, _, _, ok := mvu.HelpBoxLayout(80, 24, helpLines, mvu.HelpBoxMinWidth(80))
	if !ok || len(wrapped) == 0 {
		t.Fatalf("expected help box layout for test snapshot")
	}
	expectedLine := wrapped[0]

	e := emu.New(80, 24)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emulator write: %v", err)
	}
	screen, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	found := false
	for y := 0; y < screen.Rows; y++ {
		row := make([]rune, 0, screen.Cols)
		for x := 0; x < screen.Cols; x++ {
			cell := screen.Cells[y*screen.Cols+x]
			if cell.Rune == 0 {
				row = append(row, ' ')
				continue
			}
			row = append(row, cell.Rune)
		}
		if string(row) == "" {
			continue
		}
		if bytes.Contains([]byte(string(row)), []byte(expectedLine)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected help overlay visible after repeated renders")
	}
}
