package mvu

import (
	"io"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
)

// RenderSnapshot renders a full snapshot frame.
func RenderSnapshot(w io.Writer, snap *protocolpb.Snapshot) error {
	return render.Snapshot(w, snap)
}

// RenderSnapshotViewport renders a snapshot cropped or padded to viewport.
func RenderSnapshotViewport(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	return render.SnapshotViewport(w, snap, viewCols, viewRows)
}

// RenderSnapshotViewportNoClear renders a snapshot delta target without clear.
func RenderSnapshotViewportNoClear(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	return render.SnapshotViewportNoClear(w, snap, viewCols, viewRows)
}

// RenderSnapshotViewportDim renders a snapshot in dim grayscale style.
func RenderSnapshotViewportDim(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	return render.SnapshotViewportDim(w, snap, viewCols, viewRows)
}
