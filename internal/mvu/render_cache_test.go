package mvu

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestRenderCacheHostRoundTrip(t *testing.T) {
	var cache RenderCache
	snap := &protocolpb.Snapshot{Cols: 80, Rows: 24}
	input := cache.HostInput(HostRenderInput{Snapshot: snap})
	if input.PrevSnapshot != nil {
		t.Fatalf("expected nil prev snapshot in empty cache")
	}
	cache.CommitHost(snap, HostRenderOutput{
		Frame: FrameState{
			LastTabBarVisible:     true,
			LastTopOverlayVisible: true,
		},
	})
	next := cache.HostInput(HostRenderInput{Snapshot: snap})
	if next.PrevSnapshot != snap {
		t.Fatalf("expected cached prev snapshot")
	}
	if !next.Frame.LastTopOverlayVisible {
		t.Fatalf("expected cached frame state")
	}
}

func TestRenderCacheTopOverlayAndReset(t *testing.T) {
	var cache RenderCache
	snap := &protocolpb.Snapshot{Cols: 80, Rows: 24}
	cache.CommitHost(snap, HostRenderOutput{
		Frame: FrameState{
			LastTabBarVisible:     true,
			LastTopOverlayVisible: true,
			LastDisconnect:        true,
			LastHelp:              true,
			LastWall:              false,
		},
	})
	if !cache.TopOverlayVisible() {
		t.Fatalf("expected top overlay visible after commit")
	}
	if !cache.Frame.LastDisconnect || !cache.Frame.LastHelp {
		t.Fatalf("expected full overlay flags copied")
	}
	cache.Reset()
	if cache.PrevSnapshot != nil || cache.TopOverlayVisible() {
		t.Fatalf("expected reset to clear cache")
	}
}

func TestRenderCacheCommitDisabled(t *testing.T) {
	var cache RenderCache
	snap := &protocolpb.Snapshot{Cols: 100, Rows: 30}
	cache.CommitDisabled(snap, 120, 40, true)
	if cache.PrevSnapshot != snap {
		t.Fatalf("expected disabled commit to store snapshot")
	}
	if cache.SnapshotRows() != 30 {
		t.Fatalf("expected snapshot rows=30, got %d", cache.SnapshotRows())
	}
	if !cache.Frame.LastScrollback {
		t.Fatalf("expected scrollback flag retained in disabled commit")
	}
	if cache.TopOverlayVisible() || cache.HelpVisible() {
		t.Fatalf("expected disabled commit to clear overlays")
	}
}
