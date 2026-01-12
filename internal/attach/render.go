package attach

import (
	"io"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
)

// RenderSnapshot renders a snapshot to the writer using ANSI escapes.
func RenderSnapshot(w io.Writer, snap *protocolpb.Snapshot) error {
	return render.Snapshot(w, snap)
}

// RenderSnapshotViewport renders a snapshot cropped or padded to a viewport.
func RenderSnapshotViewport(w io.Writer, snap *protocolpb.Snapshot, viewCols, viewRows int) error {
	return render.SnapshotViewport(w, snap, viewCols, viewRows)
}
