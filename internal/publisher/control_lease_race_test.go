package publisher

import (
	"sync"
	"testing"
)

func TestConcurrentTakeControlAndLeaseUpdates(t *testing.T) {
	p := New(Options{SessionID: "control-race"})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 2000; i++ {
			p.setHolder("viewer")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 2000; i++ {
			p.TakeControl()
		}
	}()
	workers.Wait()
	p.TakeControl()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.wantControl || p.holderID != HostControlID {
		t.Fatal("host control request was not retained")
	}
}

func TestHelloKeepsConcurrentResizeDimensionsTogether(t *testing.T) {
	p := New(Options{SessionID: "hello-race", SessionName: "  named  ", Cols: 1, Rows: 1, Headless: true})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for i := 1; i < 2000; i++ {
			p.Resize(i, i, nil)
		}
	}()
	for i := 0; i < 2000; i++ {
		frame := p.helloFrame()
		hello := frame.GetHello()
		if hello.GetCols() != hello.GetRows() || hello.GetClientId() != "named" || !hello.GetHeadless() || frame.GetSessionId() != "hello-race" {
			t.Error("HELLO lost resize or session metadata")
			break
		}
	}
	workers.Wait()
}
