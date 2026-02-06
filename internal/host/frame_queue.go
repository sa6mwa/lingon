package host

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
)

type frameQueue struct {
	mu         sync.Mutex
	frames     []*protocolpb.Frame
	totalBytes int
	maxBytes   int
	notify     chan struct{}
}

func newFrameQueue(maxBytes int) *frameQueue {
	q := &frameQueue{
		maxBytes: maxBytes,
		notify:   make(chan struct{}, 1),
	}
	return q
}

func (q *frameQueue) Notify() <-chan struct{} {
	return q.notify
}

func (q *frameQueue) SetMaxBytes(maxBytes int) {
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

func (q *frameQueue) Enqueue(frame, snapshot *protocolpb.Frame) {
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

func (q *frameQueue) Pop() *protocolpb.Frame {
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

func (q *frameQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.frames)
}

func (q *frameQueue) TotalBytes() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.totalBytes
}

func (q *frameQueue) compactLocked(snapshot *protocolpb.Frame) {
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

func (q *frameQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
