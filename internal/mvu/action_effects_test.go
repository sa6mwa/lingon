package mvu

import (
	"context"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/testclock"
)

func TestScheduleActionEffectPreservesExpiryAcrossDelayedInstallation(t *testing.T) {
	for _, lag := range []time.Duration{3 * time.Second, 6 * time.Second} {
		t.Run(lag.String(), func(t *testing.T) {
			clk := testclock.New()
			runtime := NewRuntime()
			runtime.ApplyAction(ContextAction{Input: ContextInput{Clock: clk}})
			effect := runtime.ApplyAction(WallAction{Input: WallInput{Visible: true, Title: "wall", Message: "message", Duration: 5 * time.Second}})
			scheduler := NewEffectScheduler(clk)
			defer scheduler.StopAll()
			clk.Add(lag)
			fired := 0
			ScheduleActionEffect(ActionEffectPlan{Scheduler: scheduler, Ctx: t.Context(), Key: EffectKeyStateExpiry, Result: effect, Callback: func(bool) { fired++ }})
			if lag < 5*time.Second {
				clk.Add(5*time.Second - lag - time.Nanosecond)
				if fired != 0 {
					t.Fatal("wall expired before its original deadline")
				}
				clk.Add(time.Nanosecond)
			} else {
				clk.Add(0)
			}
			if fired != 1 {
				t.Fatalf("wall expiry was extended by delayed timer installation: callbacks=%d", fired)
			}
		})
	}
}

func TestAbsoluteExpiryPreservesReplacementAndCancellation(t *testing.T) {
	clk := testclock.New()
	runtime := NewRuntime()
	runtime.ApplyAction(ContextAction{Input: ContextInput{Clock: clk}})
	scheduler := NewEffectScheduler(clk)
	defer scheduler.StopAll()
	fired := 0
	install := func(ctx context.Context) {
		effect := runtime.ApplyAction(WallAction{Input: WallInput{Visible: true, Title: "wall", Duration: 5 * time.Second}})
		ScheduleActionEffect(ActionEffectPlan{Scheduler: scheduler, Ctx: ctx, Key: EffectKeyStateExpiry, Result: effect, Callback: func(bool) { fired++ }})
	}
	install(t.Context())
	clk.Add(2 * time.Second)
	install(t.Context())
	clk.Add(3 * time.Second)
	if fired != 0 {
		t.Fatal("replaced expiry callback fired")
	}
	clk.Add(2 * time.Second)
	if fired != 1 {
		t.Fatal("replacement lost its deadline")
	}
	ctx, cancel := context.WithCancel(t.Context())
	install(ctx)
	cancel()
	clk.Add(5 * time.Second)
	install(t.Context())
	scheduler.StopAll()
	clk.Add(5 * time.Second)
	if fired != 1 {
		t.Fatal("cancelled or stopped expiry callback fired")
	}
}

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
