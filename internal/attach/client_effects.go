package attach

import (
	"context"

	"pkt.systems/lingon/internal/mvu"
)

func (c *Client) startEffects(ctx context.Context) func() {
	scheduler := mvu.NewEffectScheduler(c.clock())
	c.effectsMu.Lock()
	c.runCtx = ctx
	c.effects = scheduler
	c.effectsMu.Unlock()
	return scheduler.StopAll
}

// effectRuntime snapshots the initialized scheduler and its matching context.
// Owners may request redraws before the detached transport starts; publishing
// both fields under the same lock prevents readers seeing a partial runtime.
func (c *Client) effectRuntime() (*mvu.EffectScheduler, context.Context) {
	c.effectsMu.RLock()
	defer c.effectsMu.RUnlock()
	return c.effects, c.runCtx
}
