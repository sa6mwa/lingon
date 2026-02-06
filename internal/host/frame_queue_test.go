package host

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestFrameQueueCompactsToSnapshot(t *testing.T) {
	snap := snapshotFrame(10, 5)
	size := proto.Size(snap)
	if size <= 0 {
		t.Fatalf("expected snapshot size > 0")
	}

	q := newFrameQueue(size*3 - 1)
	q.Enqueue(snap, snap)
	second := snapshotFrame(10, 5)
	q.Enqueue(second, second)
	third := snapshotFrame(10, 5)
	q.Enqueue(third, third)

	if got := q.Len(); got != 1 {
		t.Fatalf("expected compacted queue len 1, got %d", got)
	}
	frame := q.Pop()
	if frame == nil || frame.GetSnapshot() == nil {
		t.Fatalf("expected snapshot after compaction")
	}
	if frame != third {
		t.Fatalf("expected most recent snapshot after compaction")
	}
}

func snapshotFrame(cols, rows int) *protocolpb.Frame {
	total := cols * rows
	runes := make([]uint32, total)
	fg := make([]uint32, total)
	bg := make([]uint32, total)
	snap := &protocolpb.Snapshot{
		Cols:  uint32(cols),
		Rows:  uint32(rows),
		Runes: runes,
		Fg:    fg,
		Bg:    bg,
	}
	return &protocolpb.Frame{
		SessionId: "test",
		Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
	}
}
