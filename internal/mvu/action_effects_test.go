package mvu

import (
	"context"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestScheduleActionEffectStopsOnZeroDelay(t *testing.T) {
	scheduler := NewEffectScheduler(clock.New())
	defer scheduler.StopAll()
	ScheduleActionEffect(ActionEffectPlan{
		Scheduler: scheduler,
		Key:       EffectKeyStateExpiry,
		Result:    ActionResult{},
	})
}

func TestScheduleActionEffectInvokesCallback(t *testing.T) {
	clk := clock.NewMock()
	scheduler := NewEffectScheduler(clk)
	defer scheduler.StopAll()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan bool, 1)
	ScheduleActionEffect(ActionEffectPlan{
		Scheduler: scheduler,
		Ctx:       ctx,
		Key:       EffectKeyStateExpiry,
		Result: ActionResult{
			Delay:     time.Second,
			ForceFull: true,
		},
		Callback: func(forceFull bool) {
			done <- forceFull
		},
	})
	clk.Add(1100 * time.Millisecond)

	select {
	case got := <-done:
		if !got {
			t.Fatalf("expected callback forceFull=true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected callback invocation")
	}
}
