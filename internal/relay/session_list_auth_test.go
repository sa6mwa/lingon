package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestSessionListStopsPublishingAfterAuthorityRevocation(t *testing.T) {
	for _, action := range []string{"credentials", "logout", "deleted"} {
		t.Run(action, func(t *testing.T) {
			store, users := NewStore(), NewUserStore()
			user, err := SeedTestUser(users)
			if err != nil {
				t.Fatal(err)
			}
			access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			srv := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
			mock := clock.NewMock()
			srv.sessions.clock = mock
			ts := newTestServer(t, srv.Handler())
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
				HTTPHeader: http.Header{"Authorization": {"Bearer " + access.Token}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			data, _ := proto.Marshal(&protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientType: "android", ClientId: "list-only"}}})
			if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
				t.Fatal(err)
			}
			first, err := readFrame(ctx, conn, wsReadLimit)
			if err != nil || first.GetSessions() == nil {
				t.Fatalf("initial list: %v %v", first, err)
			}
			switch action {
			case "credentials":
				srv.InvalidateReloadedUsers([]string{user.Username}, time.Now())
			case "logout":
				store.DeleteAccessToken(access.Token)
			case "deleted":
				users.Delete(user.Username)
			}
			store.CreateSession(Session{ID: "private-after-reset", Username: user.Username, Name: "private", Status: "active", CreatedAt: time.Now(), LastActiveAt: time.Now()})
			host := &fakeConn{id: "fresh-host", role: RoleHost, sessionID: "private-after-reset", scope: ShareScopeControl}
			if err := srv.Hub.RegisterHost(host, host.sessionID, 80, 24); err != nil {
				t.Fatal(err)
			}
			srv.notifySessions(user.Username)
			mock.Add(sessionNotifyDelay)
			frame, err := readFrame(ctx, conn, wsReadLimit)
			if err == nil {
				t.Fatalf("revoked connection received an update: %v", frame)
			}
			if ctx.Err() != nil {
				t.Fatal("authority revocation did not close the websocket")
			}
		})
	}
}

func TestSessionListRevocationClosesOnlyTheRevokedUserAndFencesRegistration(t *testing.T) {
	store, users := NewStore(), NewUserStore()
	users.Upsert(User{Username: "first"})
	users.Upsert(User{Username: "other"})
	srv := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
	register := func(username string) (*wsConn, *http.Request, func()) {
		t.Helper()
		access, err := store.CreateAccessToken(username, DefaultAccessTokenTTL, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/ws/client", nil)
		req.Header.Set("Authorization", "Bearer "+access.Token)
		ws := &wsConn{done: make(chan struct{})}
		unregister, err := srv.registerSessionList(ws, username, req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(unregister)
		return ws, req, unregister
	}
	first, revokedRequest, unregisterFirst := register("first")
	other, _, _ := register("other")
	srv.InvalidateReloadedUsers([]string{"first"}, time.Now())
	select {
	case <-first.done:
	default:
		t.Fatal("session-list socket survived credential invalidation")
	}
	select {
	case <-other.done:
		t.Fatal("unrelated user was disconnected")
	default:
	}
	late := &wsConn{done: make(chan struct{})}
	if _, err := srv.registerSessionList(late, "first", revokedRequest); err == nil {
		t.Fatal("revoked handshake registered after credential invalidation")
	}
	fresh, _, _ := register("first")
	unregisterFirst()
	srv.InvalidateReloadedUsers([]string{"first"}, time.Now())
	select {
	case <-fresh.done:
	default:
		t.Fatal("cleanup of an old connection removed the freshly authenticated one")
	}
}

func TestSessionListShutdownClosesAllUsersAndFencesRegistration(t *testing.T) {
	store, users := NewStore(), NewUserStore()
	srv := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
	defer srv.Close("cleanup")
	var sockets []*wsConn
	var requests []*http.Request
	for _, username := range []string{"first", "other"} {
		users.Upsert(User{Username: username})
		access, err := store.CreateAccessToken(username, DefaultAccessTokenTTL, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/ws/client", nil)
		req.Header.Set("Authorization", "Bearer "+access.Token)
		ws := &wsConn{done: make(chan struct{})}
		unregister, err := srv.registerSessionList(ws, username, req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(unregister)
		sockets = append(sockets, ws)
		requests = append(requests, req)
	}
	srv.Close("shutdown")
	for i, ws := range sockets {
		select {
		case <-ws.done:
		default:
			t.Errorf("user %d session-list socket survived shutdown", i)
		}
	}
	late := &wsConn{done: make(chan struct{})}
	if unregister, err := srv.registerSessionList(late, "first", requests[0]); err == nil {
		unregister()
		t.Error("authenticated handshake registered after shutdown")
	}
	srv.Close("shutdown") // Closing twice and deferred unregister must be safe.
}

func TestSessionListShutdownClosesWebsocketAndReturnsHandler(t *testing.T) {
	store, users := NewStore(), NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatal(err)
	}
	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
	returned := make(chan struct{})
	handler := srv.Handler()
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		handler.ServeHTTP(w, r)
	}))
	defer ts.Close()
	defer srv.Close("cleanup")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	data, err := proto.Marshal(&protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientType: "android", ClientId: "shutdown-list"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatal(err)
	}
	first, err := readFrame(ctx, conn, wsReadLimit)
	if err != nil || first.GetSessions() == nil {
		t.Fatalf("initial list: %v %v", first, err)
	}
	srv.Close("shutdown")
	if frame, err := readFrame(ctx, conn, wsReadLimit); err == nil {
		t.Fatalf("socket survived shutdown: %v", frame)
	}
	if ctx.Err() != nil {
		t.Fatal("shutdown did not close the websocket before the transport deadline")
	}
	select {
	case <-returned:
	case <-ctx.Done():
		t.Fatal("session-list handler did not return after shutdown")
	}
}

func TestSessionListRegistrationConcurrentWithShutdownCannotSurvive(t *testing.T) {
	store, users := NewStore(), NewUserStore()
	users.Upsert(User{Username: "owner"})
	access, err := store.CreateAccessToken("owner", DefaultAccessTokenTTL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws/client", nil)
	req.Header.Set("Authorization", "Bearer "+access.Token)
	for i := 0; i < 100; i++ {
		srv := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
		ws := &wsConn{done: make(chan struct{})}
		start := make(chan struct{})
		registered := make(chan error, 1)
		closed := make(chan struct{})
		unregistered := make(chan struct{})
		go func() {
			defer close(unregistered)
			<-start
			unregister, err := srv.registerSessionList(ws, "owner", req)
			registered <- err
			<-closed
			if unregister != nil {
				unregister()
			}
		}()
		go func() {
			<-start
			srv.Close("shutdown")
			close(closed)
		}()
		close(start)
		err := <-registered
		<-closed
		<-unregistered
		if err == nil {
			select {
			case <-ws.done:
			default:
				t.Errorf("iteration %d: concurrent registration survived shutdown", i)
			}
		}
	}
}
