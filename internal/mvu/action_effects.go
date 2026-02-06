package mvu

import "context"

// ActionEffectPlan describes scheduler integration for one action result.
type ActionEffectPlan struct {
	Scheduler *EffectScheduler
	Ctx       context.Context
	Key       string
	Result    ActionResult
	Callback  func(forceFull bool)
}

// ScheduleActionEffect schedules action-driven redraw work via MVU effect scheduler.
func ScheduleActionEffect(plan ActionEffectPlan) {
	if plan.Scheduler == nil {
		return
	}
	if plan.Result.Delay <= 0 {
		plan.Scheduler.Stop(plan.Key)
		return
	}
	if plan.Ctx == nil {
		return
	}
	plan.Scheduler.Schedule(plan.Ctx, plan.Key, plan.Result.Delay, func() {
		if plan.Callback != nil {
			plan.Callback(plan.Result.ForceFull)
		}
	})
}
