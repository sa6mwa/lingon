package session

import (
	"context"
	"io"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/pslog"
)

func TestRemoteReconnectUsesLockedContextSnapshot(t *testing.T) {
	m := newRemoteManager(remoteOptions{Clock: clock.NewMock(), Logger: pslog.NoopLogger()})
	view := &remoteView{id: "remote", visible: true, starting: true, ctx: t.Context(), stdout: io.Discard}
	m.views[view.id] = view
	sessions := []remoteSessionInfo{{ID: view.id, Status: "active"}}
	m.sessions = sessions
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 5000 {
			m.mu.Lock()
			view.ctx = context.Background()
			view.stdout = io.Discard
			m.mu.Unlock()
		}
	})
	workers.Go(func() {
		<-start
		for range 5000 {
			m.mu.Lock()
			view.awaiting = false
			m.mu.Unlock()
			m.applySessions(sessions)
		}
	})
	close(start)
	workers.Wait()
	if !m.HasSession(view.id) || view.running {
		t.Fatal("metadata observation changed the pending reconnect session")
	}
}

func TestRemoteScrollbackUsesLockedClientSnapshot(t *testing.T) {
	m := newRemoteManager(remoteOptions{Clock: clock.NewMock(), Logger: pslog.NoopLogger()})
	view := &remoteView{id: "remote"}
	m.views[view.id] = view
	client := &attach.Client{}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 5000 {
			m.mu.Lock()
			view.client = client
			m.mu.Unlock()
		}
	})
	workers.Go(func() {
		<-start
		for range 5000 {
			if offset := m.ScrollbackOffset(view.id); offset != 0 {
				t.Errorf("empty scrollback offset = %d", offset)
				return
			}
		}
	})
	close(start)
	workers.Wait()
}
