package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

func replayTestHub(tb testing.TB, count int) (*Hub, *fakeConn) {
	tb.Helper()
	logger := pslog.NewWithOptions(context.Background(), io.Discard, pslog.Options{Mode: pslog.ModeStructured})
	hub := NewHub(logger)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "replay", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "replay", 80, 24); err != nil {
		tb.Fatal(err)
	}
	for range count {
		if err := hub.HandleHostFrame(context.Background(), host, &protocolpb.Frame{
			SessionId: "replay", Payload: &protocolpb.Frame_Diff{Diff: &protocolpb.Diff{Cols: 80, Rows: 24}},
		}); err != nil {
			tb.Fatal(err)
		}
	}
	return hub, host
}

func TestWSReplayLargeBurstPreservesWelcomeReplayLiveOrder(t *testing.T) {
	const count = 300
	frames := make(chan *protocolpb.Frame, count+1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer peer.CloseNow()
		for range count + 1 {
			frame, err := readFrame(r.Context(), peer, wsReadLimit)
			if err != nil {
				return
			}
			frames <- frame
		}
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := newWSConn("client", RoleClient, "replay", ShareScopeControl, raw, nil)
	defer conn.closeNow()
	hub, host := replayTestHub(t, count)
	hub.registerClient(conn, "replay", "client", false, true)
	observer := &fakeConn{id: "observer", role: RoleClient, sessionID: "replay", scope: ShareScopeView}
	hub.RegisterClient(observer, "replay", "observer", false)

	// Hold the transport writer, not a hub lock. Replay admission and live
	// broadcasting must complete even when this client's network cannot drain.
	conn.sendMu.Lock()
	if err := conn.Send(ctx, frameWelcome(false, 80, 24, "", "replay")); err != nil {
		conn.sendMu.Unlock()
		t.Fatal(err)
	}
	err = hub.HandleClientFrame(ctx, conn, &protocolpb.Frame{
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{LastSeq: 1}},
	})
	if err == nil {
		err = hub.HandleHostFrame(ctx, host, &protocolpb.Frame{Payload: &protocolpb.Frame_Diff{Diff: &protocolpb.Diff{Cols: 80, Rows: 24}}})
	}
	observed := observer.sentFrame(0)
	conn.sendMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if observed.GetSeq() != count+1 {
		t.Fatal("stalled replay blocked live delivery to another client")
	}
	select {
	case <-conn.done:
		t.Fatal("healthy replay client closed before its transport writer could drain")
	default:
	}
	for i := range count + 1 {
		select {
		case frame := <-frames:
			if i == 0 {
				if frame.GetWelcome() == nil {
					t.Fatal("welcome must precede replay")
				}
			} else if frame.Seq != uint64(i+1) {
				t.Fatalf("frame %d: seq=%d, want %d", i, frame.Seq, i+1)
			}
		case <-ctx.Done():
			t.Fatal("incomplete replay/live delivery")
		}
	}
}

// BenchmarkWSReplay measures admission plus actual websocket delivery. The
// short case also runs on the pre-fix implementation for a comparable baseline.
func BenchmarkWSReplay(b *testing.B) {
	for _, count := range []int{64, 300} {
		name := "short"
		if count == 300 {
			name = "burst"
		}
		b.Run(name, func(b *testing.B) {
			delivered := make(chan struct{}, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				peer, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer peer.CloseNow()
				for {
					for range count - 1 {
						if _, err := readFrame(r.Context(), peer, wsReadLimit); err != nil {
							return
						}
					}
					delivered <- struct{}{}
				}
			}))
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			raw, _, err := websocket.Dial(ctx, ts.URL, nil)
			if err != nil {
				b.Fatal(err)
			}
			conn := newWSConn("client", RoleClient, "replay", ShareScopeControl, raw, nil)
			defer conn.closeNow()
			hub, _ := replayTestHub(b, count)
			hub.RegisterClient(conn, "replay", "client", false)
			hello := &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{LastSeq: 1}}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := hub.HandleClientFrame(ctx, conn, hello); err != nil {
					b.Fatal(err)
				}
				select {
				case <-delivered:
				case <-conn.done:
					b.Fatal("replay disconnected")
				case <-ctx.Done():
					b.Fatal(ctx.Err())
				}
			}
			b.StopTimer()
		})
	}
}
