package session

import (
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestRemoteManagerConcurrentUpdatesPreserveEmptyGrace(t *testing.T) {
	clk := clock.NewMock()
	manager := newRemoteManager(remoteOptions{Clock: clk})
	sessions := []remoteSessionInfo{{ID: "live", Name: "Live", Status: "active"}}
	manager.applySessions(sessions)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, empty := range []bool{false, true} {
		workers.Add(1)
		go func(empty bool) {
			defer workers.Done()
			<-start
			for i := 0; i < 1000; i++ {
				if empty {
					manager.applySessions(nil)
				} else {
					manager.applySessions([]remoteSessionInfo{{ID: "live", Name: "Live", Status: "active"}})
				}
			}
		}(empty)
	}
	close(start)
	workers.Wait()
	assertSessionCount := func(want int) {
		t.Helper()
		if got := len(manager.Sessions()); got != want {
			t.Fatalf("session count = %d, want %d", got, want)
		}
	}
	assertSessionCount(1)
	clk.Add(4 * time.Second)
	manager.applySessions(nil)
	assertSessionCount(1)
	clk.Add(time.Second)
	manager.applySessions(nil)
	assertSessionCount(0)
}
