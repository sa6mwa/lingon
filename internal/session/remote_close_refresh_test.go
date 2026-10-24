package session

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/pslog"
)

func remoteCloseRefreshFixture(t *testing.T) (*remoteManager, *remoteView, *atomic.Int32) {
	t.Helper()
	requests := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `[{"id":"remote","status":"active"}]`)
	}))
	t.Cleanup(server.Close)
	m := newRemoteManager(remoteOptions{Endpoint: server.URL, Token: "fixture", Clock: clock.NewMock(), Logger: pslog.NoopLogger()})
	view := &remoteView{id: "remote", ctx: t.Context(), awaiting: true, runID: 2}
	m.views[view.id] = view
	return m, view, requests
}

func TestRemoteCloseRefreshUsesCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name             string
		runID            uint64
		closed, replaced bool
		want             int32
	}{
		{"current", 2, false, false, 1},
		{"obsolete run", 1, false, false, 0},
		{"replaced view", 2, false, true, 0},
		{"closed session", 2, true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, view, requests := remoteCloseRefreshFixture(t)
			view.sessionClosed = tc.closed
			if tc.replaced {
				m.views[view.id] = &remoteView{id: view.id, runID: 3}
			}
			m.handleViewClosed(view, tc.runID, errors.New("connection lost"))
			if got := requests.Load(); got != tc.want {
				t.Fatalf("refresh HTTP requests = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRemoteCloseRefreshConcurrentSessionPublication(t *testing.T) {
	// Expose simultaneous callbacks without changing the enclosing cgroup's
	// aggregate 1 CPU / 3 GiB verification budget.
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	m, view, requests := remoteCloseRefreshFixture(t)
	start := make(chan struct{})
	closed := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			m.handleViewClosed(view, 2, errors.New("connection lost"))
		}
		close(closed)
	}()
	go func() {
		defer workers.Done()
		<-start
		for {
			select {
			case <-closed:
				return
			default:
			}
			m.applySessions([]remoteSessionInfo{{ID: view.id, Status: "active"}})
			runtime.Gosched()
		}
	}()
	close(start)
	workers.Wait()
	if requests.Load() != 1000 {
		t.Fatalf("current closes lost refreshes: %d", requests.Load())
	}
}
