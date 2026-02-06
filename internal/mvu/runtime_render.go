package mvu

import (
	"fmt"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
)

// RuntimeHostFrameInput is one runtime-backed host render request.
type RuntimeHostFrameInput struct {
	Snapshot     *protocolpb.Snapshot
	Cols         int
	Rows         int
	Cursor       Cursor
	Now          time.Time
	ForceFull    bool
	SuppressTabs bool
	Cache        *RenderCache
}

// RuntimeHostFrameOutput is one runtime-backed host render result.
type RuntimeHostFrameOutput struct {
	Rendered   HostRenderOutput
	TabDelay   time.Duration
	StateDelay time.Duration
}

// RenderHostFrame renders one host frame using runtime state and render cache.
func (r *Runtime) RenderHostFrame(in RuntimeHostFrameInput) (RuntimeHostFrameOutput, error) {
	out := RuntimeHostFrameOutput{}
	if in.Snapshot == nil {
		return out, nil
	}
	now := in.Now
	if now.IsZero() {
		now = r.now()
	}
	state := r.State()
	renderState, resolveOpts, tabDelay := r.PrepareRenderState(state, in.Cursor, now, RenderStateOptions{
		SuppressTabs: in.SuppressTabs,
	})
	input := HostRenderInput{
		Snapshot:  in.Snapshot,
		Cols:      in.Cols,
		Rows:      in.Rows,
		Cursor:    in.Cursor,
		State:     renderState,
		Now:       now,
		Resolve:   resolveOpts,
		ForceFull: in.ForceFull,
	}
	if in.Cache != nil {
		input = in.Cache.HostInput(input)
	}
	rendered, err := RenderHost(input)
	if err != nil {
		return out, err
	}
	if in.Cache != nil {
		if rendered.ComposedSnapshot == nil {
			return out, fmt.Errorf("mvu invariant violation: missing composed host snapshot")
		}
		in.Cache.CommitHost(rendered.ComposedSnapshot, rendered)
	}
	out.Rendered = rendered
	out.TabDelay = tabDelay
	out.StateDelay = r.ExpiryDelay(now)
	return out, nil
}

// RuntimeAttachFrameInput is one runtime-backed attach render request.
type RuntimeAttachFrameInput struct {
	Snapshot          *protocolpb.Snapshot
	Cols              int
	Rows              int
	Cursor            Cursor
	Now               time.Time
	ForceFull         bool
	SuppressTabs      bool
	ForceTabsVisible  bool
	ScrollbackVisible bool
	Cache             *RenderCache
}

// RuntimeAttachFrameOutput is one runtime-backed attach render result.
type RuntimeAttachFrameOutput struct {
	Rendered   AttachRenderOutput
	TabDelay   time.Duration
	StateDelay time.Duration
}

// RenderAttachFrame renders one attach frame using runtime state and render cache.
func (r *Runtime) RenderAttachFrame(in RuntimeAttachFrameInput) (RuntimeAttachFrameOutput, error) {
	out := RuntimeAttachFrameOutput{}
	if in.Snapshot == nil {
		return out, nil
	}
	now := in.Now
	if now.IsZero() {
		now = r.now()
	}
	state := r.State()
	renderState, resolveOpts, tabDelay := r.PrepareRenderState(state, in.Cursor, now, RenderStateOptions{
		SuppressTabs:     in.SuppressTabs,
		ForceTabsVisible: in.ForceTabsVisible,
	})
	input := AttachRenderInput{
		Snapshot:          in.Snapshot,
		Cols:              in.Cols,
		Rows:              in.Rows,
		Cursor:            in.Cursor,
		State:             renderState,
		Now:               now,
		Resolve:           resolveOpts,
		ForceFull:         in.ForceFull,
		ScrollbackVisible: in.ScrollbackVisible,
	}
	if in.Cache != nil {
		input = in.Cache.AttachInput(input)
	}
	rendered, err := RenderAttach(input)
	if err != nil {
		return out, err
	}
	if in.Cache != nil {
		if rendered.ComposedSnapshot == nil {
			return out, fmt.Errorf("mvu invariant violation: missing composed attach snapshot")
		}
		in.Cache.CommitAttach(rendered.ComposedSnapshot, rendered)
	}
	out.Rendered = rendered
	out.TabDelay = tabDelay
	out.StateDelay = r.ExpiryDelay(now)
	return out, nil
}

// RuntimeDisabledFrameInput is one runtime-backed disabled render request.
type RuntimeDisabledFrameInput struct {
	Snapshot          *protocolpb.Snapshot
	Cols              int
	Rows              int
	Cursor            Cursor
	Now               time.Time
	ScrollbackVisible bool
	Cache             *RenderCache
}

// RuntimeDisabledFrameOutput is one runtime-backed disabled render result.
type RuntimeDisabledFrameOutput struct {
	Rendered DisabledRenderOutput
	TabDelay time.Duration
}

// RenderDisabledFrame renders one disabled frame using runtime state.
func (r *Runtime) RenderDisabledFrame(in RuntimeDisabledFrameInput) (RuntimeDisabledFrameOutput, error) {
	out := RuntimeDisabledFrameOutput{}
	if in.Snapshot == nil {
		return out, nil
	}
	now := in.Now
	if now.IsZero() {
		now = r.now()
	}
	rendered, err := RenderDisabled(DisabledRenderInput{
		Snapshot: in.Snapshot,
		Cols:     in.Cols,
		Rows:     in.Rows,
		Cursor:   in.Cursor,
		State:    r.State(),
		Now:      now,
		Resolve:  ResolveOptions{},
	})
	if err != nil {
		return out, err
	}
	if in.Cache != nil {
		if rendered.ComposedSnapshot == nil {
			return out, fmt.Errorf("mvu invariant violation: missing composed disabled snapshot")
		}
		in.Cache.CommitDisabled(rendered.ComposedSnapshot, in.Cols, in.Rows, in.ScrollbackVisible)
	}
	out.Rendered = rendered
	out.TabDelay = r.TabBarAutoHideDelay(now)
	return out, nil
}
