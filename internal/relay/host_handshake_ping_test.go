package relay

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

type registerSignalWriter struct {
	once       sync.Once
	registered chan struct{}
}

func (w *registerSignalWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("relay.hub.host.register")) {
		w.once.Do(func() { close(w.registered) })
	}
	return len(p), nil
}

func TestHostPingAnsweredWhileRegistrationPersists(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatal(err)
	}
	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store.MarkTokenClientType(access.Token, "host")
	signal := &registerSignalWriter{registered: make(chan struct{})}
	logger := pslog.NewWithOptions(context.Background(), signal, pslog.Options{Mode: pslog.ModeStructured})
	srv := NewHTTPServer(store, users, NewAuthenticator(users), logger, nil)
	srv.DataDir = t.TempDir()
	handlerDone := make(chan struct{})
	baseHandler := srv.Handler()
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseHandler.ServeHTTP(w, r)
		if r.URL.Path == "/ws/host" {
			close(handlerDone)
		}
	}))

	store.saveMu.Lock()
	var conn *websocket.Conn
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			store.saveMu.Unlock()
			if conn != nil {
				_ = conn.CloseNow()
				select {
				case <-handlerDone:
				case <-time.After(3 * time.Second):
					t.Error("host handler did not finish after test cleanup")
				}
			}
			ts.Close()
		})
	}
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err = websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hello, err := proto.Marshal(&protocolpb.Frame{
		SessionId: "ping-during-save",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols: 80, Rows: 24, ClientType: "host",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	select {
	case <-signal.registered:
	case <-ctx.Done():
		t.Fatal("host registration did not start")
	}

	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	pingCtx, pingCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer pingCancel()
	if err := conn.Ping(pingCtx); err != nil {
		t.Fatalf("host ping was not answered while registration persistence was blocked: %v", err)
	}
	cleanup()
}
