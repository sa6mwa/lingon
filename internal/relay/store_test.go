package relay

import (
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/testutil"
)

func TestStoreSaveLoad(t *testing.T) {
	dir := testutil.TempDir(t)
	store := NewStore()
	session := Session{ID: "s1", Username: "test", CreatedAt: time.Now().UTC(), Status: "active"}
	store.CreateSession(session)

	token := ShareToken{Token: "token", SessionID: session.ID, Scope: ShareScopeView, CreatedAt: time.Now().UTC()}
	store.AddShareToken(token)

	if err := store.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if _, ok := loaded.Sessions[session.ID]; !ok {
		t.Fatalf("expected session in loaded store")
	}
	if _, ok := loaded.ShareTokens[token.Token]; !ok {
		t.Fatalf("expected token in loaded store")
	}

	statePath := filepath.Join(dir, storeFilename)
	if statePath == "" {
		t.Fatalf("expected state path")
	}
}

func TestStoreListSessionsSortsByName(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	store.CreateSession(Session{ID: "session-c", Username: "alice", Name: "Alpha", CreatedAt: now.Add(2 * time.Hour), LastActiveAt: now.Add(2 * time.Hour), Status: "active"})
	store.CreateSession(Session{ID: "session-a", Username: "alice", Name: "Charlie", CreatedAt: now, LastActiveAt: now, Status: "active"})
	store.CreateSession(Session{ID: "session-b", Username: "alice", Name: "Bravo", CreatedAt: now.Add(time.Hour), LastActiveAt: now.Add(time.Hour), Status: "active"})
	store.CreateSession(Session{ID: "session-aa", Username: "bob", Name: "Aaron", CreatedAt: now, LastActiveAt: now, Status: "active"})

	got := store.ListSessions("alice")
	gotIDs := make([]string, 0, len(got))
	for _, session := range got {
		gotIDs = append(gotIDs, session.ID)
	}
	want := []string{"session-c", "session-b", "session-a"}
	if len(gotIDs) != len(want) {
		t.Fatalf("session ids=%v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("session ids=%v, want %v", gotIDs, want)
		}
	}
}
