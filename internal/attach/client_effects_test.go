package attach

import (
	"context"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/clock"
)

func TestClientEffectPublicationSynchronizesWithRedrawScheduling(t *testing.T) {
	c := &Client{Clock: clock.NewMock()}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 2000 {
			stop := c.startEffects(context.Background())
			stop()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 2000 {
			// A zero delay cancels an effect; no timer callback or terminal
			// renderer is invoked by this metadata-publication regression.
			c.scheduleRedrawEffect("startup-publication", c.Clock.Now(), 0, false)
		}
	}()
	close(start)
	wg.Wait()
}
