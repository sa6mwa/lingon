package attach_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachLocalHeadlessLoadingTabSwitchDoesNotClear(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "waiting.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	var peersMu sync.Mutex
	var peers []net.Conn
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			peersMu.Lock()
			peers = append(peers, conn)
			peersMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-stopped
		peersMu.Lock()
		defer peersMu.Unlock()
		for _, conn := range peers {
			_ = conn.Close()
		}
	})
	h := newHarness(t)
	var activeMu sync.Mutex
	activeID := ""
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless", SessionID: "first", Cols: 80, Rows: 24,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "first"}, {ID: "second"}}, nil
		},
		SocketResolver: func(string) (string, error) { return socketPath, nil },
		OnActive:       func(id string) { activeMu.Lock(); activeID = id; activeMu.Unlock() },
	})
	t.Cleanup(sess.Cancel)
	h.Advance(time.Millisecond)
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		if !strings.Contains(screen.String(), "loading local headless") {
			return fmt.Errorf("missing initial loading status")
		}
		return nil
	})
	sess.Wait(100 * time.Millisecond)
	sess.DrainRaw()
	// Exercise both a new uncached view and an existing view still awaiting its snapshot.
	for _, next := range []string{"second", "first", "second"} {
		sess.SendCtrlL()
		sess.Send("n")
		sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
			activeMu.Lock()
			active := activeID
			activeMu.Unlock()
			if active != next || !strings.Contains(screen.String(), "loading local headless") {
				return fmt.Errorf("waiting for loading tab %s, active=%s", next, active)
			}
			return nil
		})
		sess.Wait(100 * time.Millisecond)
		raw := sess.DrainRaw()
		if strings.Contains(raw, "\x1b[1;1H\x1b[0;39;49m") {
			t.Fatalf("loading tab %s painted the base top row before its overlay: %q", next, raw)
		}
		if ptytest.HasFullRedrawANSI(raw, 24) {
			t.Fatalf("loading tab %s cleared terminal: %q", next, raw)
		}
	}
}

func TestMultiAttachLocalHeadlessPendingTabPreservesDisplayedFrame(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "ready.sock")
	listener, err := net.Listen("unix", readyPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var handlers sync.WaitGroup
	allowSecond := make(chan struct{})
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
		content := "READY_CONTENT"
		if hello.GetSessionId() == "second" {
			select {
			case <-allowSecond:
				content = "SECOND_CONTENT"
			case <-ctx.Done():
				return
			}
		}
		snap := mvu.BlankSnapshot(80, 24)
		snap.Cursor = &protocolpb.Cursor{Y: 3}
		snap.CursorVisible = true
		for i, ch := range content {
			snap.Runes[3*80+i] = uint32(ch)
		}
		frames := []*protocolpb.Frame{
			{Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{HolderClientId: hello.GetHello().GetClientId()}}},
			{Seq: 1, Payload: &protocolpb.Frame_Snapshot{Snapshot: snap}},
		}
		for _, frame := range frames {
			data, err := proto.Marshal(frame)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
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
	var servedPending chan struct{}
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		<-served
		if servedPending != nil {
			<-servedPending
		}
		handlers.Wait()
	})
	// Gate the second view's first snapshot explicitly rather than with a timer.
	pendingPath := filepath.Join(t.TempDir(), "pending.sock")
	pending, err := net.Listen("unix", pendingPath)
	if err != nil {
		t.Fatal(err)
	}
	servedPending = make(chan struct{})
	go func() { defer close(servedPending); _ = server.Serve(pending) }()
	h := newHarness(t)
	var activeMu sync.Mutex
	activeID := ""
	var secondClient *attach.Client
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless", SessionID: "first", Cols: 80, Rows: 24,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "first"}, {ID: "second"}}, nil
		},
		SocketResolver: func(id string) (string, error) {
			if id == "first" {
				return readyPath, nil
			}
			return pendingPath, nil
		},
		OnActive: func(id string) { activeMu.Lock(); activeID = id; activeMu.Unlock() },
		OnView: func(id string, client *attach.Client) {
			if id == "second" {
				activeMu.Lock()
				secondClient = client
				activeMu.Unlock()
			}
		},
	})
	t.Cleanup(sess.Cancel)
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("READY_CONTENT") {
			return fmt.Errorf("waiting for ready framebuffer")
		}
		return nil
	})
	assertNoTabSwitchFlickerAfterAction(t, sess, 24, 200*time.Millisecond, func() {
		sess.SendCtrlL()
		sess.Send("n")
	})
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		activeMu.Lock()
		active := activeID
		activeMu.Unlock()
		if active != "second" || !screen.Contains("loading local headless") || !screen.Contains("READY_CONTENT") {
			return fmt.Errorf("pending tab must retain display under loading banner, active=%s", active)
		}
		return nil
	})
	activeMu.Lock()
	client := secondClient
	activeMu.Unlock()
	if client == nil || client.HasSnapshot() {
		t.Fatal("loading framebuffer must not become the second session's snapshot")
	}
	close(allowSecond)
	sess.Eventually(2*time.Second, 10*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("SECOND_CONTENT") || screen.Contains("READY_CONTENT") || screen.Contains("loading local headless") {
			return fmt.Errorf("first snapshot must replace the retained frame and loading banner")
		}
		return nil
	})
}
