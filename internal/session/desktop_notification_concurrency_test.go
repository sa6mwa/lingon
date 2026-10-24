package session

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/desktopnotify"
)

type concurrentNotificationCounter struct{ calls atomic.Int32 }

func (n *concurrentNotificationCounter) Notify(context.Context, desktopnotify.Request) error {
	n.calls.Add(1)
	return nil
}

func TestRunnerConcurrentInactivityInitializesNotifierOnce(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	notifier := &concurrentNotificationCounter{}
	var factoryCalls atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	restore := desktopnotify.SetFactoryForTesting(func() desktopnotify.Notifier {
		factoryCalls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return notifier
	})
	defer restore()
	clk := clock.NewMock()
	r := New(Options{Clock: clk, Stdin: null, Stdout: null, Cols: 80, Rows: 24})
	r.localSessions = make(map[string]*localSession)
	r.wallNotifyAfter = make(map[string]time.Duration)
	r.wallNotifyArmed = make(map[string]bool)
	const sessions = 8
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("inactivity-%d", i)
		// Inert identities only; no terminal, shell or Run is started.
		r.localSessions[id] = &localSession{id: id, clock: clk}
		r.wallNotifyAfter[id] = time.Minute
		r.wallNotifyArmed[id] = true
	}
	start := make(chan struct{})
	var joined sync.WaitGroup
	joined.Add(sessions)
	for id := range r.localSessions {
		go func() {
			defer joined.Done()
			<-start
			r.fireLocalWallNotification(id)
		}()
	}
	close(start)
	<-entered
	close(release)
	joined.Wait()
	if got := factoryCalls.Load(); got != 1 {
		t.Errorf("notifier initialized %d times, want one", got)
	}
	if got := notifier.calls.Load(); got != sessions {
		t.Errorf("delivered %d notifications, want %d", got, sessions)
	}
}
