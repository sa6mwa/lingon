package publisher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/publisher"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/lingon/internal/sessionmanage"
	"pkt.systems/pslog"
)

func TestSessionManagementDoesNotBlockTerminalInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := relay.NewStore()
	hub := relay.NewHub(pslog.NoopLogger())
	backend := relay.NewHTTPServer(store, nil, nil, pslog.NoopLogger(), hub)
	server := httptest.NewServer(backend.Handler())
	defer server.Close()
	token, err := store.CreateAccessToken("owner", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	input := make(chan string, 1)
	pub := publisher.New(publisher.Options{Endpoint: server.URL, Token: token.Token, SessionID: "host", SessionName: "host", Cols: 80, Rows: 24, Logger: pslog.NoopLogger()})
	pub.OnStatus = func(connected bool, err error) {
		if connected {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	}
	pub.OnInput = func(data []byte) { input <- string(data) }
	pub.OnManagement = func(ctx context.Context, request *protocolpb.SessionManagement) *protocolpb.SessionManagement {
		close(entered)
		select {
		case <-release:
			return &protocolpb.SessionManagement{ResultSessionId: "child", Name: "child"}
		case <-ctx.Done():
			return &protocolpb.SessionManagement{Error: ctx.Err().Error()}
		}
	}
	done := make(chan error, 1)
	go func() { done <- pub.Run(ctx) }()
	defer func() { cancel(); <-done }()
	<-ready
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()
	ws, _, err := websocket.Dial(wsCtx, strings.Replace(server.URL, "http", "ws", 1)+"/ws/client", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err := relaywire.WriteFrame(wsCtx, ws, &protocolpb.Frame{SessionId: "host", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "input-client", WantsControl: true}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := relaywire.ReadFrame(wsCtx, ws); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := sessionmanage.Call(wsCtx, server.Client(), server.URL, token.Token, sessionmanage.Request{RequestID: "request", SessionID: "host", Kind: "create"})
		result <- err
	}()
	<-entered
	if err := relaywire.WriteFrame(wsCtx, ws, &protocolpb.Frame{SessionId: "host", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("input while launch is blocked")}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-input:
		if got != "input while launch is blocked" {
			t.Fatal(got)
		}
	case <-wsCtx.Done():
		t.Fatal("terminal reader blocked behind lifecycle worker")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
