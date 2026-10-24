package mvu

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/testclock"
)

type frameExpiryClock struct {
	testclock.Mock
	armed bool
	reads int
}

func (c *frameExpiryClock) Now() time.Time {
	if c.armed {
		c.reads++
		if c.reads == 2 {
			c.Add(6 * time.Second)
		}
	}
	return c.Mock.Now()
}

func TestRenderedWallRetainsItsExpiryWhenClockAdvancesDuringRender(t *testing.T) {
	for _, owner := range []string{"host", "attach"} {
		t.Run(owner, func(t *testing.T) {
			clk := &frameExpiryClock{Mock: testclock.New()}
			runtime := NewRuntime()
			runtime.ApplyAction(ContextAction{Input: ContextInput{Clock: clk}})
			runtime.ApplyAction(WallAction{Input: WallInput{Visible: true, Title: "wall", Message: "message", Duration: 5 * time.Second}})
			now := clk.Mock.Now()
			clk.armed = true
			snap := makeSnapshot(40, 10, 0, 1)
			var delay time.Duration
			if owner == "host" {
				frame, err := runtime.RenderHostFrame(RuntimeHostFrameInput{Snapshot: snap, Cols: 40, Rows: 10, Now: now})
				if err != nil || !frame.Rendered.Resolved.WallVisible {
					t.Fatalf("wall frame was not rendered: %v", err)
				}
				delay = frame.StateDelay
			} else {
				frame, err := runtime.RenderAttachFrame(RuntimeAttachFrameInput{Snapshot: snap, Cols: 40, Rows: 10, Now: now})
				if err != nil || !frame.Rendered.Resolved.WallVisible {
					t.Fatalf("wall frame was not rendered: %v", err)
				}
				delay = frame.StateDelay
			}
			if delay != 5*time.Second {
				t.Fatalf("rendered wall lost its original expiry: %v", delay)
			}
		})
	}
}
