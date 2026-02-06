package mvu

import "pkt.systems/lingon/internal/protocolpb"

// RenderCache stores render-time MVU state between frames.
type RenderCache struct {
	PrevSnapshot *protocolpb.Snapshot
	Frame        FrameState
}

// Reset clears snapshot/frame history.
func (c *RenderCache) Reset() {
	if c == nil {
		return
	}
	c.PrevSnapshot = nil
	c.Frame = FrameState{}
}

// SetPrevSnapshot sets previous snapshot baseline.
func (c *RenderCache) SetPrevSnapshot(snapshot *protocolpb.Snapshot) {
	if c == nil {
		return
	}
	c.PrevSnapshot = snapshot
}

// HostInput applies cached state to host render input.
func (c *RenderCache) HostInput(in HostRenderInput) HostRenderInput {
	if c == nil {
		return in
	}
	in.PrevSnapshot = c.PrevSnapshot
	in.Frame = c.Frame
	return in
}

// CommitHost stores host render output/frame for next frame.
func (c *RenderCache) CommitHost(snapshot *protocolpb.Snapshot, out HostRenderOutput) {
	if c == nil {
		return
	}
	c.PrevSnapshot = snapshot
	c.Frame = out.Frame
}

// AttachInput applies cached state to attach render input.
func (c *RenderCache) AttachInput(in AttachRenderInput) AttachRenderInput {
	if c == nil {
		return in
	}
	in.PrevSnapshot = c.PrevSnapshot
	in.Frame = c.Frame
	return in
}

// CommitAttach stores attach render output/frame for next frame.
func (c *RenderCache) CommitAttach(snapshot *protocolpb.Snapshot, out AttachRenderOutput) {
	if c == nil {
		return
	}
	c.PrevSnapshot = snapshot
	c.Frame = out.Frame
}

// TopOverlayVisible reports prior top-row overlay visibility.
func (c *RenderCache) TopOverlayVisible() bool {
	if c == nil {
		return false
	}
	return c.Frame.LastTopOverlayVisible
}

// SnapshotRows reports last rendered snapshot rows.
func (c *RenderCache) SnapshotRows() int {
	if c == nil {
		return 0
	}
	return c.Frame.LastSnapRows
}

// HelpVisible reports whether help was visible in the last rendered frame.
func (c *RenderCache) HelpVisible() bool {
	if c == nil {
		return false
	}
	return c.Frame.LastHelp
}

// CommitDisabled stores frame metadata for a disabled-rendered snapshot.
func (c *RenderCache) CommitDisabled(snapshot *protocolpb.Snapshot, cols, rows int, scrollbackVisible bool) {
	if c == nil {
		return
	}
	c.PrevSnapshot = snapshot
	c.Frame.LastRenderCols = cols
	c.Frame.LastRenderRows = rows
	c.Frame.LastSnapCols = int(snapshot.GetCols())
	c.Frame.LastSnapRows = int(snapshot.GetRows())
	c.Frame.LastScrollback = scrollbackVisible
	c.Frame.LastTopOverlayVisible = false
	c.Frame.LastConnectionVisible = false
	c.Frame.LastConnectionLen = 0
	c.Frame.LastConnectionStyle = BannerRed
	c.Frame.LastScrollbackVisible = false
	c.Frame.LastScrollbackLen = 0
	c.Frame.LastDisconnect = false
	c.Frame.LastHelp = false
	c.Frame.LastWall = false
}
