package mvu

import (
	"bytes"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
)

// DisabledRenderInput is one MVU disabled/dim render update.
type DisabledRenderInput struct {
	Snapshot *protocolpb.Snapshot
	Cols     int
	Rows     int
	Cursor   Cursor
	State    State
	Now      time.Time
	Resolve  ResolveOptions
}

// DisabledRenderOutput is one MVU disabled/dim render output.
type DisabledRenderOutput struct {
	Bytes            []byte
	Resolved         Resolved
	ComposedSnapshot *protocolpb.Snapshot
}

// RenderDisabled renders a dimmed snapshot with MVU overlays.
func RenderDisabled(in DisabledRenderInput) (DisabledRenderOutput, error) {
	var out DisabledRenderOutput
	if in.Snapshot == nil {
		return out, nil
	}
	model := NewModel(in.State, in.Cursor, in.Now, in.Resolve)
	resolved := model.ResolveState()
	composed, err := ComposeDisabledViewportSnapshot(in.Snapshot, in.Cols, in.Rows, resolved, in.Cursor)
	if err != nil {
		return out, err
	}
	var frame bytes.Buffer
	if err := renderSnapshot(&frame, nil, composed, in.Cols, in.Rows, false, resolved, in.Cursor, topOverlayRenderOptions{}); err != nil {
		return out, err
	}
	out.Bytes = frame.Bytes()
	out.Resolved = resolved
	out.ComposedSnapshot = composed
	return out, nil
}
