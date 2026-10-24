package attach

import (
	"sync"
	"testing"
)

func TestClientWebSocketAssignmentSynchronizesWithConnected(t *testing.T) {
	c := &Client{}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			c.setWebSocket(nil)
			c.clearWebSocket(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			_ = c.Connected()
		}
	}()
	wg.Wait()
}
