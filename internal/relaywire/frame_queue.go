package relaywire

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
)

// FrameQueue buffers outbound relay frames with optional byte compaction.
type FrameQueue struct {
	mu         sync.Mutex
	frames     []*protocolpb.Frame
	totalBytes int
	maxBytes   int
	notify     chan struct{}
}

// NewFrameQueue constructs a relay frame queue.
func NewFrameQueue(maxBytes int) *FrameQueue {
	q := &FrameQueue{
		maxBytes: maxBytes,
		notify:   make(chan struct{}, 1),
	}
	return q
}

// Notify returns a channel signaled when frames are enqueued.
func (q *FrameQueue) Notify() <-chan struct{} {
	return q.notify
}

// SetMaxBytes updates the queue compaction threshold.
func (q *FrameQueue) SetMaxBytes(maxBytes int) {
	if maxBytes <= 0 {
		return
	}
	q.mu.Lock()
	q.maxBytes = maxBytes
	if q.totalBytes > q.maxBytes {
		q.compactLocked(nil)
	}
	q.mu.Unlock()
}

// Enqueue appends a frame, compacting to snapshot when the queue exceeds its limit.
func (q *FrameQueue) Enqueue(frame, snapshot *protocolpb.Frame) {
	if frame == nil {
		return
	}
	frameSize := proto.Size(frame)
	snapSize := 0
	if snapshot != nil {
		snapSize = proto.Size(snapshot)
	}

	q.mu.Lock()
	q.frames = append(q.frames, frame)
	q.totalBytes += frameSize
	if q.maxBytes > 0 && q.totalBytes > q.maxBytes {
		q.compactLocked(snapshot)
		if snapshot != nil {
			q.totalBytes = snapSize
		}
	}
	q.mu.Unlock()
	q.signal()
}

// Pop removes and returns the oldest queued frame.
func (q *FrameQueue) Pop() *protocolpb.Frame {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.frames) == 0 {
		return nil
	}
	frame := q.frames[0]
	q.frames[0] = nil
	q.frames = q.frames[1:]
	q.totalBytes -= proto.Size(frame)
	if q.totalBytes < 0 {
		q.totalBytes = 0
	}
	return frame
}

// Len returns the number of queued frames.
func (q *FrameQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.frames)
}

// TotalBytes returns the approximate protobuf byte size of queued frames.
func (q *FrameQueue) TotalBytes() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.totalBytes
}

func (q *FrameQueue) compactLocked(snapshot *protocolpb.Frame) {
	if snapshot != nil {
		q.frames = q.frames[:0]
		q.frames = append(q.frames, snapshot)
		q.totalBytes = proto.Size(snapshot)
		return
	}
	if len(q.frames) == 0 {
		q.totalBytes = 0
		return
	}
	last := q.frames[len(q.frames)-1]
	q.frames = q.frames[:0]
	q.frames = append(q.frames, last)
	q.totalBytes = proto.Size(last)
}

func (q *FrameQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
