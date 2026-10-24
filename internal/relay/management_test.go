package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/lingon/internal/sessionmanage"
)

type managementHost struct {
	*fakeConn
	requests chan *protocolpb.Frame
}

func TestManagementRenameImmediatelyAfterHostHello(t *testing.T) {
	store := NewStore()
	access, err := store.CreateAccessToken("owner", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	backend := NewHTTPServer(store, nil, nil, nil, nil)
	server := newTestServer(t, backend.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(server.URL, "/ws/host"), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + access.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	for _, frame := range []*protocolpb.Frame{
		{SessionId: "early-name", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "original", Cols: 80, Rows: 24, ClientType: "host"}}},
		{SessionId: "early-name", Payload: &protocolpb.Frame_Management{Management: &protocolpb.SessionManagement{Kind: "rename", Name: "early rename", ResultSessionId: "early-name", Completed: true}}},
	} {
		if err := relaywire.WriteFrame(ctx, conn, frame); err != nil {
			t.Fatal(err)
		}
	}
	// Ping is a protocol barrier after both frames, rather than a timing-based wait.
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	session, ok := store.GetSession("early-name")
	if !ok || session.Name != "early rename" {
		t.Fatalf("registration overwrote the runtime name: %+v", session)
	}
}

func TestManagementWaitersReleaseOnCancellationAndHostReplacement(t *testing.T) {
	for _, cancelRequest := range []bool{true, false} {
		hub := NewHub(nil)
		host := &managementHost{&fakeConn{id: "old", role: RoleHost, sessionID: "host"}, make(chan *protocolpb.Frame, 1)}
		hub.RegisterHost(host, "host", 80, 24)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan string, 1)
		go func() {
			result, err := hub.manage(ctx, sessionmanage.Request{SessionID: "host", RequestID: "id", Kind: "create"})
			if err != nil {
				done <- err.Error()
			} else {
				done <- result.Error
			}
		}()
		<-host.requests
		if cancelRequest {
			cancel()
		} else {
			hub.RegisterHost(&fakeConn{id: "replacement", role: RoleHost, sessionID: "host"}, "host", 80, 24)
		}
		message := <-done
		cancel()
		if !strings.Contains(message, "outcome unknown") {
			t.Fatalf("missing uncertain outcome: %q", message)
		}
		hub.mu.Lock()
		outstanding := len(hub.management)
		hub.mu.Unlock()
		if outstanding != 0 {
			t.Fatal("management waiter leaked after interruption")
		}
	}
}

func (h *managementHost) Send(ctx context.Context, frame *protocolpb.Frame) error {
	select {
	case h.requests <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestManagementOwnerAPIAndMetadata(t *testing.T) {
	store := NewStore()
	hub := NewHub(nil)
	s := NewHTTPServer(store, nil, nil, nil, hub)
	store.CreateSession(Session{ID: "host", Name: "original", Username: "owner", Status: "active"})
	owner, _ := store.CreateAccessToken("owner", time.Hour, time.Now())
	other, _ := store.CreateAccessToken("other", time.Hour, time.Now())
	host := &managementHost{&fakeConn{id: "host-connection", role: RoleHost, sessionID: "host", scope: ShareScopeControl}, make(chan *protocolpb.Frame, 8)}
	hub.RegisterHost(host, "host", 80, 24)
	request := sessionmanage.Request{RequestID: "request", SessionID: "host", Kind: "rename", Name: "日本語 build"}
	body, _ := json.Marshal(request)
	for _, test := range []struct {
		token, share string
		status       int
	}{{"", "", 401}, {other.Token, "", 403}, {owner.Token, "true", 403}} {
		r := httptest.NewRequest(http.MethodPost, "/sessions/manage", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+test.token)
		r.Header.Set("X-Lingon-Share-Session", test.share)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("status %d want %d", w.Code, test.status)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/sessions/manage", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+owner.Token)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(w, r); close(done) }()
	frame := <-host.requests
	req := frame.GetManagement()
	if req.RequestId != "request" || req.Kind != "rename" || req.Name != request.Name {
		t.Fatalf("forwarded %+v", req)
	}
	s.handleManagementResult(context.Background(), host, &protocolpb.SessionManagement{Kind: "rename", Name: req.Name, ResultSessionId: "host", Completed: true})
	s.handleManagementResult(context.Background(), host, &protocolpb.SessionManagement{CorrelationId: req.CorrelationId, RequestId: req.RequestId, Kind: "rename", Name: req.Name, ResultSessionId: "host", Completed: true})
	<-done
	if w.Code != 200 {
		t.Fatalf("response %s", w.Body.String())
	}
	renamed, _ := store.GetSession("host")
	if renamed.ID != "host" || renamed.Name != request.Name {
		t.Fatalf("metadata %+v", renamed)
	}
	s.handleManagementResult(context.Background(), host, &protocolpb.SessionManagement{Kind: "rename", Name: "newer name", ResultSessionId: "host", Completed: true})
	s.handleManagementResult(context.Background(), host, &protocolpb.SessionManagement{CorrelationId: "old-retry", Kind: "rename", Name: req.Name, ResultSessionId: "host", Completed: true})
	current, _ := store.GetSession("host")
	if current.Name != "newer name" {
		t.Fatal("retry of a completed rename overwrote a newer display name")
	}
	hub.mu.Lock()
	history := len(hub.sessions["host"].history)
	outstanding := len(hub.management)
	hub.mu.Unlock()
	if history != 0 || outstanding != 0 {
		t.Fatal("management entered replay or leaked a waiter")
	}
}

func TestManagementRejectsClientFramesOfflineAndStaleHosts(t *testing.T) {
	hub := NewHub(nil)
	s := NewHTTPServer(NewStore(), nil, nil, nil, hub)
	client := &fakeConn{id: "share", role: RoleClient, sessionID: "host", scope: ShareScopeControl, shareToken: "control-share"}
	frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Management{Management: &protocolpb.SessionManagement{Kind: "create"}}}
	if hub.HandleClientFrame(context.Background(), client, frame) == nil {
		t.Fatal("control share bypassed owner API")
	}
	if _, err := hub.manage(context.Background(), sessionmanage.Request{SessionID: "missing", RequestID: "id", Kind: "create"}); err == nil {
		t.Fatal("offline host accepted")
	}
	s.Store.CreateSession(Session{ID: "host", Name: "original", Username: "owner"})
	old := &fakeConn{id: "old", role: RoleHost, sessionID: "host"}
	current := &fakeConn{id: "new", role: RoleHost, sessionID: "host"}
	hub.RegisterHost(old, "host", 80, 24)
	hub.RegisterHost(current, "host", 80, 24)
	s.handleManagementResult(context.Background(), old, &protocolpb.SessionManagement{Kind: "rename", Name: "stale", ResultSessionId: "host", Completed: true})
	record, _ := s.Store.GetSession("host")
	if record.Name != "original" {
		t.Fatal("stale host renamed metadata")
	}
}
