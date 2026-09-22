package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

// Run with LINGON_RELAY_CAPACITY_TEST=1 GOMAXPROCS=2 go test ./internal/relay -run TestRelayHundredHostReconnectBurst.
func TestRelayHundredHostReconnectBurst(t *testing.T) {
	if os.Getenv("LINGON_RELAY_CAPACITY_TEST") != "1" {
		t.Skip("opt-in 100-connection capacity check")
	}
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatal(err)
	}
	const hosts = 100
	now := time.Now().UTC()
	tokens := make([]string, hosts)
	for i := range hosts {
		access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, now)
		if err != nil {
			t.Fatal(err)
		}
		store.MarkTokenClientType(access.Token, "host")
		tokens[i] = access.Token
		id := fmt.Sprintf("capacity-%03d", i)
		store.CreateSession(Session{ID: id, Username: user.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
		store.SetActiveSession(ActiveSession{SessionID: id, HostConnectionID: "old-relay", LastSeenAt: now, Cols: 80, Rows: 24})
	}
	const expiredTokens = 62451
	for i := range expiredTokens {
		value := fmt.Sprintf("expired-%05d", i)
		store.AccessTokens[value] = AccessToken{
			Token: value, Username: user.Username,
			CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute),
		}
	}
	dataDir := t.TempDir()
	state, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, storeFilename), state, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err = LoadStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	logger := pslog.NewWithOptions(context.Background(), io.Discard, pslog.Options{Mode: pslog.ModeStructured})
	srv := NewHTTPServer(store, users, NewAuthenticator(users), logger, nil)
	srv.DataDir = dataDir
	ts := newTestServer(t, srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release := make(chan struct{})
	results := make(chan error, hosts)
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
				HTTPHeader: http.Header{"Authorization": {"Bearer " + tokens[i]}},
			})
			if err != nil {
				results <- fmt.Errorf("host %d dial: %w", i, err)
				return
			}
			defer conn.CloseNow()
			id := fmt.Sprintf("capacity-%03d", i)
			data, err := proto.Marshal(&protocolpb.Frame{
				SessionId: id,
				Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
					Cols: 80, Rows: 24, ClientType: "host",
				}},
			})
			if err == nil {
				err = conn.Write(ctx, websocket.MessageBinary, data)
			}
			if err != nil {
				results <- fmt.Errorf("host %d hello: %w", i, err)
				return
			}
			for {
				_, data, readErr := conn.Read(ctx)
				if readErr != nil {
					results <- fmt.Errorf("host %d initial sessions: %w", i, readErr)
					return
				}
				var frame protocolpb.Frame
				if err := proto.Unmarshal(data, &frame); err != nil {
					results <- fmt.Errorf("host %d frame: %w", i, err)
					return
				}
				if frame.GetSessions() != nil {
					break
				}
			}
			results <- nil
			<-release
		}()
	}
	var failures []error
	for range hosts {
		if err := <-results; err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		active, err := listActiveSessions(t, ts.URL, tokens[0])
		if err != nil {
			failures = append(failures, err)
		} else if len(active) != hosts {
			failures = append(failures, fmt.Errorf("active sessions = %d, want %d", len(active), hosts))
		}
		compacted, err := LoadStore(dataDir)
		if err != nil {
			failures = append(failures, err)
		} else if got := len(compacted.AccessTokens); got != hosts {
			failures = append(failures, fmt.Errorf("persisted access tokens = %d, want %d after compaction", got, hosts))
		}
	}
	close(release)
	wg.Wait()
	for _, err := range failures {
		t.Error(err)
	}
}
