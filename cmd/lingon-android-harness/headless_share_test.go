package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestHeadlessShareWaitsForItsRelayPublication(t *testing.T) {
	testHeadlessSharePublication(t, false, false)
}

func TestHeadlessShareCancellationDoesNotCreateToken(t *testing.T) {
	testHeadlessSharePublication(t, true, false)
}

func TestHeadlessShareTransportFailureDoesNotCreateToken(t *testing.T) {
	testHeadlessSharePublication(t, false, true)
}

func testHeadlessSharePublication(t *testing.T, cancelBeforePublication, disconnectBeforePublication bool) {
	t.Helper()
	// Deadline is only a deadlock guard; protocol gates prove ordering.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	subscribed := make(chan struct{})
	publish := make(chan struct{})
	disconnect := make(chan struct{})
	var ready atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "missing account authorization", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/ws/client":
			ws, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer ws.CloseNow()
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var hello protocolpb.Frame
			if err := proto.Unmarshal(data, &hello); err != nil || hello.GetHello() == nil || hello.GetSessionId() != "" {
				t.Error("expected account session-list subscription, without attaching a terminal")
				return
			}
			initial := &protocolpb.Frame{Payload: &protocolpb.Frame_Sessions{Sessions: &protocolpb.Sessions{Sessions: []*protocolpb.SessionInfo{
				{Id: "other-session", Status: "active"}, {Id: "headless-1", Status: "offline"},
			}}}}
			data, _ = proto.Marshal(initial)
			if err := ws.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
			close(subscribed)
			select {
			case <-publish:
			case <-disconnect:
				return
			case <-ctx.Done():
				return
			}
			ready.Store(true)
			initial.GetSessions().Sessions = []*protocolpb.SessionInfo{{Id: "headless-1", Status: "active"}}
			data, _ = proto.Marshal(initial)
			_ = ws.Write(ctx, websocket.MessageBinary, data)
			<-ctx.Done()
		case "/v1/share/create":
			requests.Add(1)
			if !ready.Load() {
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
			fmt.Fprint(w, `{"token":"published-share"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// Cancel websocket handlers before joining the test server.
	defer cancel()
	h := &harness{endpoint: server.URL + "/v1", access: "fixture-token", server: server}
	done := make(chan error, 1)
	go func() {
		share, err := h.createHeadlessShare(ctx, "headless-1")
		if err == nil && share.Token != "published-share" {
			err = fmt.Errorf("wrong share token: %q", share.Token)
		}
		done <- err
	}()
	select {
	case <-subscribed:
	case err := <-done:
		t.Fatalf("share attempted before relay publication: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if requests.Load() != 0 {
		t.Fatal("unrelated active session or offline target triggered share creation")
	}
	if disconnectBeforePublication {
		close(disconnect)
		if err := <-done; err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("peer disconnect must remain a transport failure: %v", err)
		}
		if requests.Load() != 0 {
			t.Fatal("failed transport created a share")
		}
		return
	}
	if cancelBeforePublication {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("publication cancellation: %v", err)
		}
		if requests.Load() != 0 {
			t.Fatal("cancelled publication created a share")
		}
		return
	}
	close(publish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("share requests = %d, want exactly one", requests.Load())
	}
}
