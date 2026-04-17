package mvu

import (
	"bytes"
	"io"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
)

// FrameState tracks prior render context required for MVU diff composition.
type FrameState struct {
	LastOverlayFull       bool
	LastTabBarVisible     bool
	LastTopOverlayVisible bool
	LastTabBarSignature   uint64
	LastConnectionVisible bool
	LastConnectionLen     int
	LastConnectionStyle   BannerStyle
	LastLoadingVisible    bool
	LastLoadingLen        int
	LastScrollbackVisible bool
	LastScrollbackLen     int

	LastDisconnect bool
	LastHelp       bool
	LastWall       bool

	LastRenderCols int
	LastRenderRows int
	LastSnapCols   int
	LastSnapRows   int
	LastScrollback bool
}

// HostRenderInput is one host-side MVU render update.
type HostRenderInput struct {
	PrevSnapshot *protocolpb.Snapshot
	Snapshot     *protocolpb.Snapshot
	Cols         int
	Rows         int
	Cursor       Cursor
	State        State
	Now          time.Time
	Resolve      ResolveOptions
	ForceFull    bool
	Frame        FrameState
}

// HostRenderOutput is one host-side MVU render view output.
type HostRenderOutput struct {
	Bytes            []byte
	Resolved         Resolved
	Frame            FrameState
	ComposedSnapshot *protocolpb.Snapshot
}

// AttachRenderInput is one attach-side MVU render update.
type AttachRenderInput struct {
	PrevSnapshot      *protocolpb.Snapshot
	Snapshot          *protocolpb.Snapshot
	Cols              int
	Rows              int
	Cursor            Cursor
	State             State
	Now               time.Time
	Resolve           ResolveOptions
	ForceFull         bool
	ScrollbackVisible bool
	Frame             FrameState
}

// AttachRenderOutput is one attach-side MVU render view output.
type AttachRenderOutput struct {
	Bytes            []byte
	Resolved         Resolved
	Frame            FrameState
	ComposedSnapshot *protocolpb.Snapshot
}

type topOverlayRenderOptions struct {
	SkipTabBar         bool
	PrevConnectionLen  int
	PrevScrollbackLen  int
	PrevBannerRowOwned bool
}

func bannerRowOwned(resolved Resolved) bool {
	return !resolved.TabBarVisible && (resolved.ConnectionVisible || resolved.LoadingVisible)
}

func frameBannerRowOwned(frame FrameState) bool {
	return !frame.LastTabBarVisible && (frame.LastConnectionVisible || frame.LastLoadingVisible)
}

func topStatusStable(prev FrameState, resolved Resolved) bool {
	if prev.LastConnectionVisible != resolved.ConnectionVisible {
		return false
	}
	if prev.LastLoadingVisible != resolved.LoadingVisible {
		return false
	}
	if prev.LastScrollbackVisible != resolved.ScrollbackVisible {
		return false
	}
	if resolved.ConnectionVisible {
		if prev.LastConnectionStyle != resolved.State.ConnectionStyle {
			return false
		}
	}
	return true
}

// RenderHost executes the host MVU update+view pipeline for one frame.
func RenderHost(in HostRenderInput) (HostRenderOutput, error) {
	out := HostRenderOutput{}
	model := NewModel(in.State, in.Cursor, in.Now, in.Resolve)
	resolved := model.ResolveState()
	tabSig := uint64(0)
	if resolved.TabBarVisible {
		tabSig = tabBarSignature(in.Cols, resolved.State)
	}
	skipTabBar := resolved.TabBarVisible &&
		in.Frame.LastTabBarVisible &&
		in.Frame.LastTopOverlayVisible &&
		in.Frame.LastTabBarSignature == tabSig &&
		topStatusStable(in.Frame, resolved) &&
		!in.ForceFull &&
		in.PrevSnapshot != nil
	composed, err := ComposeViewportSnapshot(in.Snapshot, in.Cols, in.Rows, resolved, in.Cursor)
	if err != nil {
		return out, err
	}
	prev := in.PrevSnapshot
	clear := prev != nil && (int(prev.GetCols()) != int(composed.GetCols()) || int(prev.GetRows()) != int(composed.GetRows()))
	if in.Frame.LastRenderCols > 0 && in.Frame.LastRenderCols != in.Cols {
		clear = true
	}
	if in.Frame.LastRenderRows > 0 && in.Frame.LastRenderRows != in.Rows {
		clear = true
	}
	if in.Frame.LastSnapCols > 0 && in.Frame.LastSnapCols != int(composed.Cols) {
		clear = true
	}
	if in.Frame.LastSnapRows > 0 && in.Frame.LastSnapRows != int(composed.Rows) {
		clear = true
	}
	if (in.Frame.LastRenderCols > 0 || in.Frame.LastRenderRows > 0 || in.Frame.LastSnapCols > 0 || in.Frame.LastSnapRows > 0) &&
		in.Frame.LastScrollback != resolved.ScrollbackVisible {
		clear = true
	}
	if clear {
		prev = nil
	}
	skipTabBar = skipTabBar && prev != nil
	var frame bytes.Buffer
	if err := renderSnapshot(&frame, prev, composed, in.Cols, in.Rows, in.ForceFull, resolved, in.Cursor, topOverlayRenderOptions{
		SkipTabBar:         skipTabBar,
		PrevConnectionLen:  in.Frame.LastConnectionLen,
		PrevScrollbackLen:  in.Frame.LastScrollbackLen,
		PrevBannerRowOwned: frameBannerRowOwned(in.Frame),
	}); err != nil {
		return out, err
	}
	out.Bytes = frame.Bytes()
	out.Resolved = resolved
	out.ComposedSnapshot = composed
	out.Frame = in.Frame
	out.Frame.LastOverlayFull = resolved.FullOverlayVisible
	out.Frame.LastTabBarVisible = resolved.TabBarVisible
	out.Frame.LastTopOverlayVisible = resolved.TopOverlayVisible
	out.Frame.LastConnectionVisible = resolved.ConnectionVisible
	out.Frame.LastConnectionLen = len(resolved.State.ConnectionMessage)
	out.Frame.LastConnectionStyle = resolved.State.ConnectionStyle
	out.Frame.LastLoadingVisible = resolved.LoadingVisible
	out.Frame.LastLoadingLen = len(resolved.State.LoadingMessage)
	out.Frame.LastScrollbackVisible = resolved.ScrollbackVisible
	out.Frame.LastScrollbackLen = len(resolved.State.ScrollbackMessage)
	out.Frame.LastRenderCols = in.Cols
	out.Frame.LastRenderRows = in.Rows
	out.Frame.LastSnapCols = int(composed.Cols)
	out.Frame.LastSnapRows = int(composed.Rows)
	out.Frame.LastScrollback = resolved.ScrollbackVisible
	if resolved.TabBarVisible {
		out.Frame.LastTabBarSignature = tabSig
	} else {
		out.Frame.LastTabBarSignature = 0
	}
	return out, nil
}

// RenderAttach executes the attach MVU update+view pipeline for one frame.
func RenderAttach(in AttachRenderInput) (AttachRenderOutput, error) {
	out := AttachRenderOutput{}
	model := NewModel(in.State, in.Cursor, in.Now, in.Resolve)
	resolved := model.ResolveState()
	tabSig := uint64(0)
	if resolved.TabBarVisible {
		tabSig = tabBarSignature(in.Cols, resolved.State)
	}
	composed, err := ComposeViewportSnapshot(in.Snapshot, in.Cols, in.Rows, resolved, in.Cursor)
	if err != nil {
		return out, err
	}
	prev := in.PrevSnapshot
	clear := in.Frame.LastRenderCols == 0 || in.Frame.LastRenderRows == 0 ||
		in.Frame.LastRenderCols != in.Cols || in.Frame.LastRenderRows != in.Rows ||
		in.Frame.LastSnapCols != int(composed.Cols) || in.Frame.LastSnapRows != int(composed.Rows) ||
		in.Frame.LastScrollback != in.ScrollbackVisible
	if clear {
		prev = nil
	}
	skipTabBar := resolved.TabBarVisible &&
		in.Frame.LastTabBarVisible &&
		in.Frame.LastTopOverlayVisible &&
		in.Frame.LastTabBarSignature == tabSig &&
		topStatusStable(in.Frame, resolved) &&
		!in.ForceFull &&
		prev != nil
	var frame bytes.Buffer
	if err := renderSnapshot(&frame, prev, composed, in.Cols, in.Rows, in.ForceFull, resolved, in.Cursor, topOverlayRenderOptions{
		SkipTabBar:         skipTabBar,
		PrevConnectionLen:  in.Frame.LastConnectionLen,
		PrevScrollbackLen:  in.Frame.LastScrollbackLen,
		PrevBannerRowOwned: frameBannerRowOwned(in.Frame),
	}); err != nil {
		return out, err
	}
	out.Bytes = frame.Bytes()
	out.Resolved = resolved
	out.ComposedSnapshot = composed
	out.Frame = in.Frame
	out.Frame.LastTopOverlayVisible = resolved.TopOverlayVisible
	out.Frame.LastTabBarVisible = resolved.TabBarVisible
	out.Frame.LastConnectionVisible = resolved.ConnectionVisible
	out.Frame.LastConnectionLen = len(resolved.State.ConnectionMessage)
	out.Frame.LastConnectionStyle = resolved.State.ConnectionStyle
	out.Frame.LastLoadingVisible = resolved.LoadingVisible
	out.Frame.LastLoadingLen = len(resolved.State.LoadingMessage)
	out.Frame.LastScrollbackVisible = resolved.ScrollbackVisible
	out.Frame.LastScrollbackLen = len(resolved.State.ScrollbackMessage)
	if resolved.TabBarVisible {
		out.Frame.LastTabBarSignature = tabSig
	} else {
		out.Frame.LastTabBarSignature = 0
	}
	out.Frame.LastDisconnect = resolved.DisconnectVisible
	out.Frame.LastHelp = resolved.HelpVisible
	out.Frame.LastWall = resolved.WallVisible
	out.Frame.LastRenderCols = in.Cols
	out.Frame.LastRenderRows = in.Rows
	out.Frame.LastSnapCols = int(composed.Cols)
	out.Frame.LastSnapRows = int(composed.Rows)
	out.Frame.LastScrollback = in.ScrollbackVisible
	return out, nil
}

func renderSnapshot(w io.Writer, prev, snap *protocolpb.Snapshot, cols, rows int, forceClear bool, resolved Resolved, cursor Cursor, top topOverlayRenderOptions) error {
	if forceClear {
		return render.SnapshotViewport(w, snap, cols, rows)
	}
	topRowOwned := resolved.TabBarVisible || resolved.ConnectionVisible || resolved.LoadingVisible
	if topRowOwned {
		var err error
		if prev != nil {
			err = render.SnapshotViewportDeltaMaskTopRow(w, prev, snap, cols, rows)
		} else {
			err = render.SnapshotViewportNoClearMaskTopRow(w, snap, cols, rows)
		}
		if err != nil {
			return err
		}
		var overlay []byte
		if top.SkipTabBar {
			overlay = ComposeTopOverlayResolvedNoTabsPadded(cols, cursor, resolved, top.PrevConnectionLen, top.PrevScrollbackLen)
		} else {
			overlay = ComposeTopOverlayResolved(cols, cursor, resolved)
		}
		if len(overlay) == 0 {
			return nil
		}
		if bannerRowOwned(resolved) {
			var row bytes.Buffer
			ClearRow(&row, cols, 1, resolved.State.Theme)
			if _, err := w.Write(row.Bytes()); err != nil {
				return err
			}
		}
		_, err = w.Write(overlay)
		return err
	}
	if top.PrevBannerRowOwned {
		if err := render.SnapshotViewportNoClear(w, snap, cols, 1); err != nil {
			return err
		}
		if rows > 1 {
			if err := render.SnapshotViewportDeltaSkipTopRow(w, prev, snap, cols, rows); err != nil {
				return err
			}
		}
		var cursorBuf bytes.Buffer
		WriteCursor(&cursorBuf, cursor)
		_, err := w.Write(cursorBuf.Bytes())
		return err
	}
	if prev != nil {
		return render.SnapshotViewportDelta(w, prev, snap, cols, rows)
	}
	return render.SnapshotViewportNoClear(w, snap, cols, rows)
}
