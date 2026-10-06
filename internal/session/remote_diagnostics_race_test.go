package session

import (
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/pslog"
	"sync"
	"testing"
)

func TestRemoteViewDiagnosticsConcurrentClose(t *testing.T) {
	m := newRemoteManager(remoteOptions{Clock: clock.NewMock(), Logger: pslog.NoopLogger()})
	view := &remoteView{id: "diagnostic", visible: true, awaiting: true, runID: 1}
	m.views[view.id] = view
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			m.mu.Lock()
			view.running = true
			view.visible = false
			view.hiddenAt = m.clock.Now()
			m.mu.Unlock()
			m.handleViewClosed(view, 1, nil)
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			m.logViewState("concurrent", view, nil)
		}
	}()
	close(start)
	workers.Wait()
	m.mu.Lock()
	running := view.running
	m.mu.Unlock()
	if running {
		t.Fatal("closed view remained running")
	}
}

func BenchmarkRemoteViewDiagnostics(b *testing.B) {
	m := newRemoteManager(remoteOptions{Clock: clock.NewMock(), Logger: pslog.NoopLogger()})
	view := &remoteView{id: "diagnostic", visible: true, runID: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.logViewState("send_input", view, nil)
	}
}
