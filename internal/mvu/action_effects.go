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
	if plan.Result.Delay <= 0 && plan.Result.Deadline.IsZero() {
		plan.Scheduler.Stop(plan.Key)
		return
	}
	if plan.Ctx == nil {
		return
	}
	callback := func() {
		if plan.Callback != nil {
			plan.Callback(plan.Result.ForceFull)
		}
	}
	if !plan.Result.Deadline.IsZero() {
		plan.Scheduler.ScheduleAt(plan.Ctx, plan.Key, plan.Result.Deadline, callback)
	} else {
		plan.Scheduler.Schedule(plan.Ctx, plan.Key, plan.Result.Delay, callback)
	}
}
