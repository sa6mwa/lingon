package session

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestSessionsStreamRemovesExitedHost(t *testing.T) {
	root := testutil.SetXDGConfigEnv(t)
	configDir := filepath.Join(root, "lingon")
	tlsDir := filepath.Join(configDir, "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(configDir, "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = configDir

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	shellPath := "/bin/sh"

	inAR, inAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin A pipe: %v", err)
	}
	outAR, outAW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout A pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inAR.Close()
		_ = inAW.Close()
		_ = outAR.Close()
		_ = outAW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, outAR)
	}()

	inBR, inBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin B pipe: %v", err)
	}
	outBR, outBW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout B pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inBR.Close()
		_ = inBW.Close()
		_ = outBR.Close()
		_ = outBW.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, outBR)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runnerA := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_a",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inAR,
		Stdout:     outAW,
		DisableRaw: true,
	})
	runnerB := New(Options{
		Endpoint:   endpoint,
		Token:      access.Token,
		SessionID:  "session_b",
		Cols:       80,
		Rows:       24,
		Shell:      shellPath,
		Publish:    true,
		Stdin:      inBR,
		Stdout:     outBW,
		DisableRaw: true,
	})

	runErrA := make(chan error, 1)
	runErrB := make(chan error, 1)
	go func() {
		runErrA <- runnerA.Run(ctx)
	}()
	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_a")
	}, runErrA)
	go func() {
		runErrB <- runnerB.Run(ctx)
	}()
	waitUntilAll(t, 5*time.Second, func() bool {
		return hub.HasHost("session_b")
	}, runErrA, runErrB)

	tlsPool, err := tlsmgr.LoadLocalCARoots(tlsDir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: tlsPool, MinVersion: tls.VersionTLS12}},
	}
	wsConn, _, err := websocket.Dial(ctx, wsURL(endpoint, "/ws/client"), &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("ws client dial: %v", err)
	}
	t.Cleanup(func() {
		_ = wsConn.Close(websocket.StatusNormalClosure, "bye")
	})
	hello := &protocolpb.Frame{
		SessionId: "session_a",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "client_test",
			Cols:         80,
			Rows:         24,
			WantsControl: false,
			ClientType:   "client",
		}},
	}
	data, err := proto.Marshal(hello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := wsConn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("send client hello: %v", err)
	}

	updates := make(chan []remoteSessionInfo, 8)
	go func() {
		defer close(updates)
		for {
			_, msg, err := wsConn.Read(ctx)
			if err != nil {
				return
			}
			var frame protocolpb.Frame
			if err := proto.Unmarshal(msg, &frame); err != nil {
				continue
			}
			if sessions := frame.GetSessions(); sessions != nil {
				updates <- toRemoteSessionsFromProto(sessions.GetSessions())
			}
		}
	}()

	waitUntilAll(t, 5*time.Second, func() bool {
		for {
			select {
			case sessions := <-updates:
				if len(sessions) >= 2 {
					return true
				}
			default:
				return false
			}
		}
	}, runErrA, runErrB)

	_, _ = inBW.Write([]byte("exit\n"))
	waitUntilAll(t, 5*time.Second, func() bool {
		for {
			select {
			case sessions := <-updates:
				if len(sessions) == 1 && sessions[0].ID == "session_a" {
					return true
				}
			default:
				return false
			}
		}
	}, runErrA)

	_ = inAW.Close()
	_ = inBW.Close()
	cancel()

	select {
	case <-runErrA:
	case <-time.After(5 * time.Second):
		t.Fatalf("session A did not exit")
	}
	select {
	case <-runErrB:
	case <-time.After(5 * time.Second):
		t.Fatalf("session B did not exit")
	}
}

func wsURL(base, path string) string {
	ws := strings.Replace(base, "http://", "ws://", 1)
	ws = strings.Replace(ws, "https://", "wss://", 1)
	return ws + path
}
