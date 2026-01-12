package host

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestPublisherBufferOverflowResetsToSnapshot(t *testing.T) {
	p := NewPublisher(PublishOptions{
		SessionID:   "session",
		BufferLines: 1,
	})

	snap1 := &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'A', 'B'}}
	snap2 := &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'C', 'D'}}

	p.Publish([]byte("one\n"), snap1)
	p.Publish([]byte("two\n"), snap2)

	if len(p.buffer) != 1 {
		t.Fatalf("buffer size = %d, want 1", len(p.buffer))
	}
	if p.buffer[0].frame.GetSnapshot() == nil {
		t.Fatalf("expected snapshot after overflow")
	}
}

func TestPublisherSnapshotCountsLinesWithoutNewlines(t *testing.T) {
	p := NewPublisher(PublishOptions{
		SessionID:   "session",
		BufferLines: 10,
	})

	snap := &protocolpb.Snapshot{Cols: 2, Rows: 3, Runes: []uint32{'A', 'B', 'C', 'D', 'E', 'F'}}

	p.Publish([]byte("abc"), snap)

	if p.bufferUsed != 3 {
		t.Fatalf("bufferUsed = %d, want 3", p.bufferUsed)
	}
	if len(p.buffer) != 1 {
		t.Fatalf("buffer size = %d, want 1", len(p.buffer))
	}
}
