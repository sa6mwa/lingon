package ptytest

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestWSRecorderWaitForFrameUsesEvents(t *testing.T) {
	r := NewWSRecorder()
	old, _ := proto.Marshal(&protocolpb.Frame{SessionId: "old"})
	r.record("host", DirServerToClient, old)
	checked := make(chan struct{}, 1)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		done <- r.WaitForFrame(ctx, func(rec FrameRecord) bool {
			if rec.SessionID == "old" {
				checked <- struct{}{}
			}
			return rec.SessionID == "wanted"
		})
	}()
	<-checked
	want, _ := proto.Marshal(&protocolpb.Frame{SessionId: "wanted"})
	r.record("host", DirServerToClient, want)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := r.WaitForFrame(ctx, func(FrameRecord) bool { return false }); !errors.Is(err, context.Canceled) {
		t.Fatal("wait did not honor cancellation", err)
	}
}
