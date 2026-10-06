package attach_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

// Hold readiness's backoff-reset registration after its view-state update,
// without synchronizing that update with the activating renderer. All waits
// use the test's injected clock or owned cancellation context.
type readinessRegistrationClock struct {
	*clock.MockClock
	ctx     context.Context
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *readinessRegistrationClock) AfterFunc(d time.Duration, fn func()) *clock.Timer {
	if d == 2*time.Second && c.armed.Load() {
		c.once.Do(func() { close(c.reached) })
		select {
		case <-c.release:
		case <-c.ctx.Done():
		}
	}
	return c.MockClock.AfterFunc(d, fn)
}

func TestLocalHeadlessReadyActivationCannotLeaveLoadingBanner(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ready.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var handlers sync.WaitGroup
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		hello := &protocolpb.Frame{}
		if err := proto.Unmarshal(raw, hello); err != nil {
			return
		}
		snapshot := mvu.BlankSnapshot(80, 24)
		snapshot.Cursor = &protocolpb.Cursor{Y: 3}
		for i, ch := range "READY_" + hello.GetSessionId() {
			snapshot.Runes[3*80+i] = uint32(ch)
		}
		for _, frame := range []*protocolpb.Frame{
			{Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{HolderClientId: hello.GetHello().GetClientId()}}},
			{Seq: 1, Payload: &protocolpb.Frame_Snapshot{Snapshot: snapshot}},
		} {
			data, err := proto.Marshal(frame)
			if err != nil || conn.Write(ctx, websocket.MessageBinary, data) != nil {
				return
			}
		}
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	})}
	served := make(chan struct{})
	go func() { defer close(served); _ = server.Serve(listener) }()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		<-served
		handlers.Wait()
	})
	h := newHarness(t)
	clk := &readinessRegistrationClock{MockClock: clock.NewMock(), ctx: ctx, reached: make(chan struct{}), release: make(chan struct{})}
	allowReady := make(chan struct{})
	firstReady := make(chan struct{})
	secondEntered := make(chan struct{})
	secondReady := make(chan struct{})
	var armSize atomic.Bool
	var activeMu sync.Mutex
	secondActivations := 0
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless", SessionID: "first", Cols: 80, Rows: 24, Clock: clk,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "first"}, {ID: "second"}}, nil
		},
		SocketResolver: func(string) (string, error) { return socket, nil },
		OnActive: func(id string) {
			if id == "second" {
				activeMu.Lock()
				secondActivations++
				activeMu.Unlock()
			}
		},
		OnView: func(id string, client *attach.Client) {
			ready := client.OnReady
			if id == "first" {
				client.OnReady = func() { ready(); close(firstReady) }
				return
			}
			client.OnReady = func() {
				close(secondEntered)
				select {
				case <-allowReady:
					ready()
				case <-ctx.Done():
				}
				close(secondReady)
			}
			size := client.TermSize
			client.TermSize = func() (int, int) {
				if armSize.CompareAndSwap(true, false) {
					// A cached frame swap owns its render lock here, but not
					// the multi-view state lock. Advance mock time while the
					// real readiness callback updates that view's state.
					clk.armed.Store(true)
					timer := clk.NewTimer(200 * time.Millisecond)
					close(allowReady)
					select {
					case <-timer.C:
					case <-ctx.Done():
					}
					timer.Stop()
				}
				return size()
			}
		},
	})
	t.Cleanup(sess.Cancel)
	t.Cleanup(cancel)
	expectFrame := func(id string) {
		t.Helper()
		sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
			if !screen.Contains("READY_" + id) {
				return fmt.Errorf("waiting for %s frame; screen:\n%s", id, screen.String())
			}
			return nil
		})
	}
	expectFrame("first")
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(ptytest.Screen) error {
		select {
		case <-firstReady:
			return nil
		default:
			return fmt.Errorf("first view has not completed readiness")
		}
	})
	switchTab := func() {
		assertNoTabSwitchFlickerAfterAction(t, sess, 24, 500*time.Millisecond, func() {
			sess.SendCtrlL()
			sess.Send("n")
		})
	}
	switchTab()
	expectFrame("second")
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(ptytest.Screen) error {
		select {
		case <-secondEntered:
			return nil
		default:
			return fmt.Errorf("second snapshot has not reached readiness")
		}
	})
	switchTab()
	expectFrame("first")
	armSize.Store(true)
	switchTab()
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(ptytest.Screen) error {
		activeMu.Lock()
		defer activeMu.Unlock()
		if secondActivations < 2 {
			return fmt.Errorf("cached activation has not completed")
		}
		return nil
	})
	select {
	case <-clk.reached:
	default:
		t.Fatal("readiness did not update state during the cached frame swap")
	}
	close(clk.release)
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		select {
		case <-secondReady:
		default:
			return fmt.Errorf("second readiness has not completed")
		}
		if !screen.Contains("READY_second") || screen.Contains("loading local headless") {
			return fmt.Errorf("ready second must replace its loading banner; screen:\n%s", screen.String())
		}
		return nil
	})
}
